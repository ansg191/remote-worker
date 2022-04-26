package compute

import (
	"context"
	"io"

	"github.com/pkg/errors"

	"golang.anshulg.com/popcorntime/go_encoder/api/proto"
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

	IsReady(ctx context.Context) (bool, error)
	IsReadyChan(ctx context.Context) <-chan error
}

type WorkerFactory interface {
	Create(ctx context.Context) (Worker, error)
}
