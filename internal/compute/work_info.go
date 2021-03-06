package compute

import (
	"context"
	"errors"

	"go.uber.org/zap"
)

type WorkRunFunc[T, U any] func(ctx context.Context, logger *zap.Logger, req T, instance Worker) (U, error)

type WorkInfo interface {
	getCtx() context.Context
	getReq() any
	setRes(res any)
	setErr(err error)
	run(ctx context.Context, logger *zap.Logger, req any, instance Worker) (any, error)
}

type GenericWorkInfo[T, U any] struct {
	Ctx     context.Context
	Request T
	Result  chan U
	Err     chan error
	Run     WorkRunFunc[T, U]
}

func (w *GenericWorkInfo[T, U]) getCtx() context.Context {
	return w.Ctx
}

func (w *GenericWorkInfo[T, U]) getReq() any {
	return w.Request
}

func (w *GenericWorkInfo[T, U]) setRes(res any) {
	w.Result <- res.(U)
}

func (w *GenericWorkInfo[T, U]) setErr(err error) {
	w.Err <- err
}

func (w *GenericWorkInfo[T, U]) run(ctx context.Context, logger *zap.Logger, req any, worker Worker) (any, error) {
	switch req.(type) {
	case T:
		return w.Run(ctx, logger, req.(T), worker)
	case nil:
		var x T // Pass default value
		return w.Run(ctx, logger, x, worker)
	default:
		return nil, errors.New("invalid request type")
	}
}

func NewWorkInfo[T, U any](ctx context.Context, request T, runFunc WorkRunFunc[T, U]) *GenericWorkInfo[T, U] {
	return &GenericWorkInfo[T, U]{
		Ctx:     ctx,
		Request: request,
		Result:  make(chan U, 1),
		Err:     make(chan error, 1),
		Run:     runFunc,
	}
}
