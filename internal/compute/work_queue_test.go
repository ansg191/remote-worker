package compute

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	m "github.com/launchdarkly/go-test-helpers/v2/matchers"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewQueue(t *testing.T) {
	type args struct {
		pool    Pool
		maxSize int
	}
	tests := []struct {
		name   string
		args   args
		expect WorkQueue
	}{
		{
			name: "1",
			args: args{
				pool:    nil,
				maxSize: 5,
			},
			expect: &DefaultWorkQueue{
				pool:    nil,
				workers: nil,
				maxSize: 5,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			qI := NewQueue(zap.NewNop(), test.args.pool, test.args.maxSize)

			q, ok := qI.(*DefaultWorkQueue)
			if !ok {
				t.FailNow()
			}

			expected := test.expect.(*DefaultWorkQueue)

			m.For(t, "maxSize").Assert(q.maxSize, m.Equal(expected.maxSize))
		})
	}
}

func TestDefaultWorkQueue_GetMaxSize(t *testing.T) {
	tests := []struct {
		name    string
		maxSize int
		expect  int
	}{
		{
			name:    "0",
			maxSize: 0,
			expect:  0,
		},
		{
			name:    "1",
			maxSize: 1,
			expect:  1,
		},
		{
			name:    "100",
			maxSize: 100,
			expect:  100,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			qI := NewQueue(zap.NewNop(), nil, test.maxSize)

			m.For(t, "maxSize").Assert(qI.GetMaxSize(), m.Equal(test.expect))
		})
	}
}

func TestDefaultWorkQueue_SetMaxSize(t *testing.T) {
	tests := []struct {
		name    string
		maxSize int
		expect  int
	}{
		{
			name:    "0",
			maxSize: 0,
			expect:  0,
		},
		{
			name:    "1",
			maxSize: 1,
			expect:  1,
		},
		{
			name:    "100",
			maxSize: 100,
			expect:  100,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			qI := NewQueue(zap.NewNop(), nil, 0)

			qI.SetMaxSize(test.maxSize)

			m.For(t, "maxSize").Assert(qI.GetMaxSize(), m.Equal(test.expect))
		})
	}
}

func TestDefaultWorkQueue_Add(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	mWorker := NewMockWorker(ctrl)
	mPool := NewMockPool(ctrl)

	mPool.EXPECT().
		GetWorker(gomock.Any()).
		Return(mWorker, nil).
		AnyTimes()
	mPool.EXPECT().
		ReturnWorker(gomock.Eq(mWorker)).
		AnyTimes()

	mWorker.EXPECT().
		IsReadyChan(gomock.Any()).
		DoAndReturn(func(ctx context.Context) <-chan error {
			ch := make(chan error)
			go func() {
				ch <- nil
			}()
			return ch
		}).
		AnyTimes()

	mWorker.EXPECT().
		Connect(gomock.Any()).
		Return(nil).
		AnyTimes()

	mWorker.EXPECT().
		Equals(gomock.Eq(mWorker)).
		Return(true).
		AnyTimes()

	tests := []struct {
		name   string
		work   *GenericWorkInfo[any, any]
		result any
		err    error
	}{
		{
			name: "string",
			work: NewWorkInfo[any, any](
				context.Background(),
				"Hello, World",
				func(ctx context.Context, logger *zap.Logger, req any, worker Worker) (any, error) {
					m.For(t, "req").Assert(req, m.Equal("Hello, World"))
					m.For(t, "worker").Assert(worker, m.Equal(mWorker))
					return req, nil
				}),
			result: "Hello, World",
			err:    nil,
		},
		{
			name: "int",
			work: NewWorkInfo(
				context.Background(),
				5,
				func(ctx context.Context, logger *zap.Logger, req any, worker Worker) (any, error) {
					m.For(t, "req").Assert(req, m.Equal(5))
					m.For(t, "worker").Assert(worker, m.Equal(mWorker))
					return req, nil
				}),
			result: 5,
			err:    nil,
		},
		{
			name: "err",
			work: NewWorkInfo(
				context.Background(),
				5,
				func(ctx context.Context, logger *zap.Logger, req any, worker Worker) (any, error) {
					m.For(t, "req").Assert(req, m.Equal(5))
					m.For(t, "worker").Assert(worker, m.Equal(mWorker))
					return 0, errors.New("err")
				}),
			result: 0,
			err:    errors.New("err"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q := NewQueue(logger, mPool, 5)
			q.Add(test.work)
			q.Wait()

			select {
			case res := <-test.work.Result:
				m.For(t, "result").Require(res, m.Equal(test.result))
			case err := <-test.work.Err:
				m.For(t, "err").Require(err, m.Equal(test.err))
			}
		})
	}
}
