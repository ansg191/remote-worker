package compute

import (
	"sync"

	"go.uber.org/zap"
)

type WorkQueue interface {
	// Add new job to the WorkQueue
	Add(info WorkInfo)
	// Wait for all jobs to complete
	Wait()
	// GetMaxSize returns maximum number of allowed concurrent instances
	GetMaxSize() int
	// SetMaxSize sets maximum number of allowed concurrent instances
	SetMaxSize(size int)
}

type InstanceWorkQueue struct {
	logger *zap.Logger

	wg   sync.WaitGroup // WaitGroup to manage all jobs
	pool InstancePool   // InstancePool to get instances from

	workQueue chan WorkInfo // workQueue used as a thread-safe FIFO queue

	mtx       sync.Mutex  // mtx is a mutex for instances.
	instances []*Instance // Instances in-use taken from the pool.

	maxSize int // Maximum number of active in-use instances
}

func NewQueue(logger *zap.Logger, pool InstancePool, maxSize int) WorkQueue {
	wq := &InstanceWorkQueue{
		logger:    logger,
		pool:      pool,
		maxSize:   maxSize,
		workQueue: make(chan WorkInfo, 1024),
	}

	go wq.run()

	return wq
}

func (q *InstanceWorkQueue) run() {
	for {
		if len(q.instances) < q.maxSize {
			// Wait for work
			work := <-q.workQueue

			q.logger.Debug("Work received", zap.Any("req", work.getReq()))

			q.mtx.Lock()
			q.wg.Add(1)

			instance, err := q.pool.GetAvailableInstance(work.getCtx())
			if err != nil {
				q.logger.Error("Error getting instance from pool", zap.Error(err))
				work.setErr(err)
				q.wg.Done()
				q.mtx.Unlock()
				continue
			}

			q.logger.Debug("Instance retrieved for work", zap.Any("req", work.getReq()), zap.String("instanceId", instance.Id))

			q.instances = append(q.instances, instance)

			q.mtx.Unlock()

			go func() {
				defer func(instance *Instance) {
					q.wg.Done()
					q.instances = removeItem(q.instances, instance)
					_ = instance.Close()
				}(instance)

				q.logger.Debug("Waiting for instance ready", zap.Any("req", work.getReq()), zap.String("instanceId", instance.Id))
				err := <-instance.IsReadyChan(work.getCtx())
				if err != nil {
					work.setErr(err)
					return
				}

				err = instance.Refresh(work.getCtx())
				if err != nil {
					work.setErr(err)
					return
				}

				q.logger.Info("Starting work", zap.Any("req", work.getReq()), zap.String("instanceId", instance.Id))
				result, err := work.run(work.getCtx(), q.logger, work.getReq(), instance)
				if err != nil {
					work.setErr(err)
				} else {
					work.setRes(result)
				}
				q.logger.Info("Work finished", zap.Any("req", work.getReq()), zap.String("instanceId", instance.Id))
			}()
		}
	}
}

func (q *InstanceWorkQueue) Add(info WorkInfo) {
	q.logger.Debug("Adding job to WorkQueue", zap.Any("req", info.getReq()))
	q.workQueue <- info
}

func (q *InstanceWorkQueue) Wait() {
	for len(q.workQueue) > 0 {
		q.wg.Wait()
	}
}

func (q *InstanceWorkQueue) GetMaxSize() int {
	return q.maxSize
}

func (q *InstanceWorkQueue) SetMaxSize(size int) {
	q.maxSize = size
}
