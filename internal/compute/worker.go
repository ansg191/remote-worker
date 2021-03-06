package compute

import (
	"context"
	"io"

	"github.com/pkg/errors"

	"github.com/ansg191/remote-worker/api/proto"
)

var (
	ErrClosed = errors.New("worker closed")
)

type Worker interface {
	io.Closer

	Equals(other Worker) bool

	Connect(ctx context.Context) error

	Worker() proto.WorkerServiceClient
	Job() proto.JobServiceClient

	IsReady(ctx context.Context, opts ...ReadyOptionsFunc) (bool, error)
	IsReadyChan(ctx context.Context, opts ...ReadyOptionsFunc) <-chan error
}

type WorkerFactory interface {
	Create(ctx context.Context) (Worker, error)
}
