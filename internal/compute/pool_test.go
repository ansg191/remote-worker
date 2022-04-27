package compute

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	m "github.com/launchdarkly/go-test-helpers/v2/matchers"
	"go.uber.org/zap/zaptest"
)

func TestNewPool(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	mWorkerFactory := NewMockWorkerFactory(ctrl)

	poolI := NewPool(logger, mWorkerFactory)

	pool, ok := poolI.(*DefaultPool)
	if !ok {
		t.Fatal("pool not *DefaultPool")
	}

	m.For(t, "allWorkers").Assert(pool.allInstances, m.BeNil())
	m.For(t, "availableWorkers").Assert(pool.availableInstances, m.BeNil())
}

func TestDefaultPool_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("empty pool", func(t *testing.T) {
		mWorkerFactory := NewMockWorkerFactory(ctrl)

		pool := NewPool(logger, mWorkerFactory)
		err := pool.Close()
		m.For(t, "close err").Assert(err, m.BeNil())
	})

	t.Run("1 worker pool", func(t *testing.T) {
		mWorker := NewMockWorker(ctrl)
		mWorker.EXPECT().
			Close().
			Return(nil)

		mWorkerFactory := NewMockWorkerFactory(ctrl)
		mWorkerFactory.EXPECT().
			Create(gomock.Any()).
			Return(mWorker, nil)

		pool := NewPool(logger, mWorkerFactory)
		_, err := pool.GetWorker(context.Background())
		m.For(t, "get worker err").Require(err, m.BeNil())

		err = pool.Close()
		m.For(t, "close err").Assert(err, m.BeNil())
	})

	t.Run("2 worker pool", func(t *testing.T) {
		mWorker := NewMockWorker(ctrl)
		mWorker.EXPECT().
			Close().
			Return(nil)
		mWorker2 := NewMockWorker(ctrl)
		mWorker2.EXPECT().
			Close().
			Return(nil)

		mWorkerFactory := NewMockWorkerFactory(ctrl)
		mWorkerFactory.EXPECT().
			Create(gomock.Any()).
			Return(mWorker2, nil).
			Times(1).
			After(mWorkerFactory.EXPECT().
				Create(gomock.Any()).
				Return(mWorker, nil).
				Times(1))

		pool := NewPool(logger, mWorkerFactory)
		_, err := pool.GetWorker(context.Background())
		m.For(t, "get worker err").Require(err, m.BeNil())

		_, err = pool.GetWorker(context.Background())
		m.For(t, "get worker 2 err").Require(err, m.BeNil())

		err = pool.Close()
		m.For(t, "close err").Assert(err, m.BeNil())
	})

	t.Run("1 worker pool twice", func(t *testing.T) {
		mWorker := NewMockWorker(ctrl)
		mWorker.EXPECT().
			Close().
			Return(nil)
		mWorker.EXPECT().
			IsReady(gomock.Any()).
			Return(true, nil)
		mWorker.EXPECT().
			Equals(gomock.Any()).
			Return(true)

		mWorkerFactory := NewMockWorkerFactory(ctrl)
		mWorkerFactory.EXPECT().
			Create(gomock.Any()).
			Return(mWorker, nil).
			Times(1)

		pool := NewPool(logger, mWorkerFactory)
		_, err := pool.GetWorker(context.Background())
		m.For(t, "get worker err").Require(err, m.BeNil())

		pool.ReturnWorker(mWorker)

		_, err = pool.GetWorker(context.Background())
		m.For(t, "get worker 2 err").Require(err, m.BeNil())

		err = pool.Close()
		m.For(t, "close err").Assert(err, m.BeNil())
	})

	t.Run("1 worker pool err", func(t *testing.T) {
		expectedErr := errors.New("worker close error")

		mWorker := NewMockWorker(ctrl)
		mWorker.EXPECT().
			Close().
			Return(expectedErr)

		mWorkerFactory := NewMockWorkerFactory(ctrl)
		mWorkerFactory.EXPECT().
			Create(gomock.Any()).
			Return(mWorker, nil)

		pool := NewPool(logger, mWorkerFactory)
		_, err := pool.GetWorker(context.Background())
		m.For(t, "get worker err").Require(err, m.BeNil())

		err = pool.Close()
		m.For(t, "close err").Assert(err, m.Equal(expectedErr))
	})

}

func TestDefaultPool_GetWorker(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("new worker", func(t *testing.T) {
		mWorker := NewMockWorker(ctrl)

		mWorkerFactory := NewMockWorkerFactory(ctrl)
		mWorkerFactory.EXPECT().
			Create(gomock.Any()).
			Return(mWorker, nil)

		pool := NewPool(logger, mWorkerFactory)

		dp := pool.(*DefaultPool)

		worker, err := pool.GetWorker(context.Background())
		m.For(t, "err").Assert(err, m.BeNil())
		m.For(t, "worker").Assert(worker, m.Equal(mWorker))
		m.For(t, "allWorkers").Assert(dp.allInstances, m.Items(m.Equal(mWorker)))
		m.For(t, "availableWorkers").Assert(dp.availableInstances, m.BeNil())
	})

	t.Run("new worker err", func(t *testing.T) {
		expectedErr := errors.New("create worker error")

		mWorkerFactory := NewMockWorkerFactory(ctrl)
		mWorkerFactory.EXPECT().
			Create(gomock.Any()).
			Return(nil, expectedErr)

		pool := NewPool(logger, mWorkerFactory)
		dp := pool.(*DefaultPool)

		worker, err := pool.GetWorker(context.Background())
		m.For(t, "err").Assert(err, m.Equal(expectedErr))
		m.For(t, "worker").Assert(worker, m.BeNil())
		m.For(t, "allWorkers").Assert(dp.allInstances, m.BeNil())
		m.For(t, "availableWorkers").Assert(dp.availableInstances, m.BeNil())
	})

	t.Run("2 new worker", func(t *testing.T) {
		mWorker := NewMockWorker(ctrl)
		mWorker2 := NewMockWorker(ctrl)

		mWorkerFactory := NewMockWorkerFactory(ctrl)
		mWorkerFactory.EXPECT().
			Create(gomock.Any()).
			Return(mWorker2, nil).
			Times(1).
			After(mWorkerFactory.EXPECT().
				Create(gomock.Any()).
				Return(mWorker, nil).
				Times(1))

		pool := NewPool(logger, mWorkerFactory)

		dp := pool.(*DefaultPool)

		worker, err := pool.GetWorker(context.Background())
		m.For(t, "worker").For("err").Assert(err, m.BeNil())
		m.For(t, "worker").For("worker").Assert(worker, m.Equal(mWorker))

		worker, err = pool.GetWorker(context.Background())
		m.For(t, "worker2").For("err").Assert(err, m.BeNil())
		m.For(t, "worker2").For("worker").Assert(worker, m.Equal(mWorker))

		m.For(t, "allWorkers").Assert(dp.allInstances, m.Items(m.Equal(mWorker), m.Equal(mWorker2)))
		m.For(t, "availableWorkers").Assert(dp.availableInstances, m.BeNil())
	})

	t.Run("1 worker reuse", func(t *testing.T) {
		mWorker := NewMockWorker(ctrl)
		mWorker.EXPECT().
			IsReady(gomock.Any()).
			Return(true, nil)
		mWorker.EXPECT().
			Equals(gomock.Any()).
			Return(true)

		mWorkerFactory := NewMockWorkerFactory(ctrl)
		mWorkerFactory.EXPECT().
			Create(gomock.Any()).
			Return(mWorker, nil)

		pool := NewPool(logger, mWorkerFactory)

		dp := pool.(*DefaultPool)

		worker, err := pool.GetWorker(context.Background())
		m.For(t, "worker").For("err").Assert(err, m.BeNil())
		m.For(t, "worker").For("worker").Assert(worker, m.Equal(mWorker))

		pool.ReturnWorker(worker)

		worker, err = pool.GetWorker(context.Background())
		m.For(t, "worker2").For("err").Assert(err, m.BeNil())
		m.For(t, "worker2").For("worker").Assert(worker, m.Equal(mWorker))

		m.For(t, "allWorkers").Assert(dp.allInstances, m.Items(m.Equal(mWorker)))
		m.For(t, "availableWorkers").Assert(dp.availableInstances, m.Length().Should(m.Equal(0)))
	})

	t.Run("1 worker reuse fail", func(t *testing.T) {
		mWorker := NewMockWorker(ctrl)
		mWorker2 := NewMockWorker(ctrl)

		mWorker.EXPECT().
			IsReady(gomock.Any()).
			Return(false, ErrClosed)
		mWorker.EXPECT().
			Equals(gomock.Any()).
			Return(true).
			AnyTimes()

		mWorker2.EXPECT().
			Equals(gomock.Any()).
			Return(true).
			AnyTimes()

		mWorkerFactory := NewMockWorkerFactory(ctrl)
		mWorkerFactory.EXPECT().
			Create(gomock.Any()).
			Return(mWorker2, nil).
			Times(1).
			After(mWorkerFactory.EXPECT().
				Create(gomock.Any()).
				Return(mWorker, nil).
				Times(1))

		pool := NewPool(logger, mWorkerFactory)

		dp := pool.(*DefaultPool)

		worker, err := pool.GetWorker(context.Background())
		m.For(t, "worker").For("err").Assert(err, m.BeNil())
		m.For(t, "worker").For("worker").Assert(worker, m.Equal(mWorker))

		pool.ReturnWorker(worker)

		worker, err = pool.GetWorker(context.Background())
		m.For(t, "worker2").For("err").Assert(err, m.BeNil())
		m.For(t, "worker2").For("worker").Assert(worker, m.Equal(mWorker2))

		m.For(t, "allWorkers").Assert(dp.allInstances, m.Items(m.Equal(mWorker2)))
		m.For(t, "availableWorkers").Assert(dp.availableInstances, m.Length().Should(m.Equal(0)))
	})
}

func TestDefaultPool_ReturnWorker(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("return worker", func(t *testing.T) {
		mWorker := NewMockWorker(ctrl)
		mWorker.EXPECT().
			Equals(gomock.Any()).
			Return(true)

		mWorkerFactory := NewMockWorkerFactory(ctrl)
		mWorkerFactory.EXPECT().
			Create(gomock.Any()).
			Return(mWorker, nil)

		pool := NewPool(logger, mWorkerFactory)
		dp := pool.(*DefaultPool)

		worker, err := pool.GetWorker(context.Background())
		m.For(t, "err").Require(err, m.BeNil())
		m.For(t, "worker").Require(worker, m.Equal(mWorker))

		pool.ReturnWorker(worker)
		m.For(t, "allWorkers").Assert(dp.allInstances, m.Items(m.Equal(mWorker)))
		m.For(t, "availableWorkers").Assert(dp.availableInstances, m.Items(m.Equal(mWorker)))
	})

	t.Run("return non-pool worker", func(t *testing.T) {
		mWorker := NewMockWorker(ctrl)

		mWorkerFactory := NewMockWorkerFactory(ctrl)

		pool := NewPool(logger, mWorkerFactory)
		dp := pool.(*DefaultPool)

		pool.ReturnWorker(mWorker)
		m.For(t, "allWorkers").Assert(dp.allInstances, m.Length().Should(m.Equal(0)))
		m.For(t, "availableWorkers").Assert(dp.availableInstances, m.Length().Should(m.Equal(0)))
	})
}
