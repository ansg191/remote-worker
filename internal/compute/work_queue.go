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
	// GetMaxSize returns maximum number of allowed concurrent workers
	GetMaxSize() int
	// SetMaxSize sets maximum number of allowed concurrent workers
	SetMaxSize(size int)
}

type DefaultWorkQueue struct {
	logger *zap.Logger

	wg   sync.WaitGroup // WaitGroup to manage all jobs
	pool Pool           // InstancePool to get workers from

	workQueue chan WorkInfo // workQueue used as a thread-safe FIFO queue

	mtx     sync.Mutex // mtx is a mutex for workers.
	workers []Worker   // Instances in-use taken from the pool.

	maxSize int // Maximum number of active in-use workers
}

func NewQueue(logger *zap.Logger, pool Pool, maxSize int) WorkQueue {
	wq := &DefaultWorkQueue{
		logger:    logger,
		pool:      pool,
		maxSize:   maxSize,
		workQueue: make(chan WorkInfo, 1024),
	}

	go wq.run()

	return wq
}

func (q *DefaultWorkQueue) run() {
	for {
		if len(q.workers) < q.maxSize {
			// Wait for work
			work := <-q.workQueue

			q.logger.Debug("Work received", zap.Any("req", work.getReq()))

			q.mtx.Lock()
			q.wg.Add(1)

			worker, err := q.pool.GetWorker(work.getCtx())
			if err != nil {
				q.logger.Error("Error getting worker from pool", zap.Error(err))
				work.setErr(err)
				q.wg.Done()
				q.mtx.Unlock()
				continue
			}

			q.logger.Debug("Worker retrieved for work", zap.Any("req", work.getReq()))

			q.workers = append(q.workers, worker)

			q.mtx.Unlock()

			go func() {
				defer func(worker Worker) {
					q.wg.Done()
					q.workers = removeItem(q.workers, worker)
					q.pool.ReturnWorker(worker)
				}(worker)

				q.logger.Debug("Waiting for worker ready", zap.Any("req", work.getReq()))
				err := <-worker.IsReadyChan(work.getCtx())
				if err != nil {
					work.setErr(err)
					return
				}

				err = worker.Connect(work.getCtx())
				if err != nil {
					work.setErr(err)
					return
				}

				q.logger.Info("Starting work", zap.Any("req", work.getReq()))
				result, err := work.run(work.getCtx(), q.logger, work.getReq(), worker)
				if err != nil {
					work.setErr(err)
				} else {
					work.setRes(result)
				}
				q.logger.Info("Work finished", zap.Any("req", work.getReq()))
			}()
		}
	}
}

func (q *DefaultWorkQueue) Add(info WorkInfo) {
	q.logger.Debug("Adding job to WorkQueue", zap.Any("req", info.getReq()))
	q.workQueue <- info
}

func (q *DefaultWorkQueue) Wait() {
	for len(q.workQueue) > 0 {
		q.wg.Wait()
	}
}

func (q *DefaultWorkQueue) GetMaxSize() int {
	return q.maxSize
}

func (q *DefaultWorkQueue) SetMaxSize(size int) {
	q.maxSize = size
}
