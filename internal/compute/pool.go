package compute

import (
	"context"
	"io"
	"sync"

	"go.uber.org/zap"
)

type Pool interface {
	io.Closer

	GetWorker(ctx context.Context) (Worker, error)
	ReturnWorker(worker Worker)
}

type DefaultPool struct {
	logger *zap.Logger

	factory WorkerFactory

	mtx                sync.Mutex // Mutex for below slices
	allInstances       []Worker   // All Workers active in pool
	availableInstances []Worker   // All Workers available in pool
}

func NewPool(logger *zap.Logger, factory WorkerFactory) Pool {
	pool := &DefaultPool{
		logger:  logger,
		factory: factory,
	}

	return pool
}

func (p *DefaultPool) Close() (err error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.logger.Info("Pool closing", zap.Int("openWorkers", len(p.allInstances)))
	for _, instance := range p.allInstances {
		err = instance.Close()
		if err != nil {
			p.logger.Error("error closing worker", zap.Error(err))
		}
	}
	p.logger.Info("Pool closed")
	return
}

func (p *DefaultPool) GetWorker(ctx context.Context) (Worker, error) {
	p.mtx.Lock()
	//defer p.mtx.Unlock()

	if len(p.availableInstances) == 0 {
		p.logger.Debug("No worker available in pool. Creating new...")
		worker, err := p.factory.Create(ctx)
		if err != nil {
			p.mtx.Unlock()
			return nil, err
		}

		p.allInstances = append(p.allInstances, worker)

		p.logger.Debug("Worker created")
		p.mtx.Unlock()

		return worker, err
	} else {
		worker := p.availableInstances[0]
		p.availableInstances = p.availableInstances[1:]
		if _, err := worker.IsReady(ctx); err == ErrClosed {
			// Worker is already closed. Remove from pool
			p.allInstances = removeItem(p.allInstances, worker)
			p.mtx.Unlock()
			return p.GetWorker(ctx)
		}

		p.logger.Debug("Pool returning existing worker")
		p.mtx.Unlock()
		return worker, nil
	}
}

func (p *DefaultPool) ReturnWorker(worker Worker) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if find(p.allInstances, worker) < 0 {
		// Worker not from this pool, return silently
		return
	}

	p.availableInstances = append(p.availableInstances, worker)
}
