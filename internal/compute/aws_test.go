package compute

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/golang/mock/gomock"
	m "github.com/launchdarkly/go-test-helpers/v2/matchers"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func grpcServer(t *testing.T) (*net.TCPAddr, *grpc.Server) {
	t.Helper()

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}

	gsrv := grpc.NewServer()
	go func() {
		if err := gsrv.Serve(l); err != nil {
			panic(err)
		}
	}()

	return l.Addr().(*net.TCPAddr), gsrv
}

func TestNewAWSWorkerFactory(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	mAWSClient := NewMockAWSWorkerEC2Client(ctrl)

	factoryI := NewAWSWorkerFactory(logger, mAWSClient, DefaultAWSInstanceParams, 443)

	factory := factoryI.(*AWSWorkerFactory)

	m.For(t, "factory").For("input").
		Assert(factory.params, m.Equal(DefaultAWSInstanceParams))
	m.For(t, "factory").For("port").Assert(factory.port, m.Equal(uint16(443)))
}

func TestAWSWorkerFactory_Create(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("normal behavior", func(t *testing.T) {
		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			RunInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.RunInstancesOutput{
				Instances: []types.Instance{{InstanceId: aws.String("i-123456")}},
			}, nil)

		factory := NewAWSWorkerFactory(logger, mAWSClient, DefaultAWSInstanceParams, 443)

		workerI, err := factory.Create(context.Background())

		m.For(t, "create err").Assert(err, m.BeNil())

		worker := workerI.(*AWSWorker)

		m.For(t, "worker").For("client").Assert(worker.client, m.Equal(mAWSClient))
		m.For(t, "worker").For("id").Assert(worker.id, m.Equal("i-123456"))
		m.For(t, "worker").For("port").Assert(worker.port, m.Equal(uint16(443)))
	})

	t.Run("error", func(t *testing.T) {
		expectedErr := errors.New("something bad happened")

		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			RunInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, expectedErr)

		factory := NewAWSWorkerFactory(logger, mAWSClient, DefaultAWSInstanceParams, 443)

		workerI, err := factory.Create(context.Background())

		m.For(t, "create err").Assert(err, m.Equal(expectedErr))
		m.For(t, "worker").Assert(workerI, m.BeNil())
	})

	t.Run("no instance returned", func(t *testing.T) {
		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			RunInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.RunInstancesOutput{
				Instances: []types.Instance{},
			}, nil)

		factory := NewAWSWorkerFactory(logger, mAWSClient, DefaultAWSInstanceParams, 443)

		workerI, err := factory.Create(context.Background())

		m.For(t, "create err").Assert(err, m.Equal(errors.New("no instances found")))
		m.For(t, "worker").Assert(workerI, m.BeNil())
	})
}

func TestAWSWorker_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("close new worker", func(t *testing.T) {
		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			TerminateInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil)

		worker := &AWSWorker{
			logger: logger,
			client: mAWSClient,
			id:     "id",
		}

		err := worker.Close()
		m.For(t, "close err").Assert(err, m.BeNil())
	})

	t.Run("double close worker", func(t *testing.T) {
		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			TerminateInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil)

		worker := &AWSWorker{
			logger: logger,
			client: mAWSClient,
			id:     "id",
		}

		err := worker.Close()
		m.For(t, "close err").Require(err, m.BeNil())

		err = worker.Close()
		m.For(t, "close err 2").Assert(err, m.Equal(ErrClosed))
	})

	t.Run("close connected worker", func(t *testing.T) {
		addr, _ := grpcServer(t)

		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			TerminateInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil)

		worker := &AWSWorker{
			logger: logger,
			client: mAWSClient,
			id:     "id",
		}

		var err error
		worker.conn, err = grpc.Dial(addr.String(),
			grpc.WithBlock(),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}

		err = worker.Close()
		m.For(t, "close err").Require(err, m.BeNil())
	})

	t.Run("close connected worker grpc error", func(t *testing.T) {
		addr, _ := grpcServer(t)

		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			TerminateInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil)

		worker := &AWSWorker{
			logger: logger,
			client: mAWSClient,
			id:     "id",
		}

		var err error
		worker.conn, err = grpc.Dial(addr.String(),
			grpc.WithBlock(),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}

		_ = worker.conn.Close() // cause grpc error
		err = worker.Close()
		m.For(t, "close err").Require(err, m.BeNil())
	})
}

func TestAWSWorker_Equals(t *testing.T) {
	tests := []struct {
		name   string
		w1     *AWSWorker
		w2     Worker
		expect bool
	}{
		{
			name:   "same",
			w1:     &AWSWorker{id: "id"},
			w2:     &AWSWorker{id: "id"},
			expect: true,
		},
		{
			name:   "different",
			w1:     &AWSWorker{id: "id"},
			w2:     &AWSWorker{id: "id2"},
			expect: false,
		},
		{
			name:   "different2",
			w1:     &AWSWorker{id: "id2"},
			w2:     &AWSWorker{id: "id"},
			expect: false,
		},
		{
			name:   "nil",
			w1:     &AWSWorker{id: "id"},
			w2:     nil,
			expect: false,
		},
		{
			name:   "different worker",
			w1:     &AWSWorker{id: "id"},
			w2:     &MockWorker{},
			expect: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m.For(t, "equal").Assert(test.w1.Equals(test.w2), m.Equal(test.expect))
		})
	}
}

func TestAWSWorker_Connect(t *testing.T) {
	addr, _ := grpcServer(t)

	ip := addr.AddrPort().Addr()
	port := addr.AddrPort().Port()

	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("normal behavior", func(t *testing.T) {
		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil)

		worker := &AWSWorker{
			logger: logger,
			client: mAWSClient,
			id:     "id",
			port:   port,
		}

		err := worker.Connect(context.Background())
		m.For(t, "err").Assert(err, m.BeNil())
	})

	t.Run("double connect", func(t *testing.T) {
		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil).
			Times(2)

		worker := &AWSWorker{
			logger: logger,
			client: mAWSClient,
			id:     "id",
			port:   port,
		}

		err := worker.Connect(context.Background())
		m.For(t, "err").Require(err, m.BeNil())

		err = worker.Connect(context.Background())
		m.For(t, "err").Assert(err, m.BeNil())
	})

	t.Run("connect to closed", func(t *testing.T) {
		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			TerminateInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil)

		worker := &AWSWorker{
			logger: logger,
			client: mAWSClient,
			id:     "id",
			port:   port,
		}

		err := worker.Close()
		m.For(t, "close err").Require(err, m.BeNil())

		err = worker.Connect(context.Background())
		m.For(t, "err").Assert(err, m.Equal(ErrClosed))
	})

	t.Run("missing ip", func(t *testing.T) {
		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: nil,
					}}},
				},
			}, nil)

		worker := &AWSWorker{
			logger: logger,
			client: mAWSClient,
			id:     "id",
			port:   port,
		}

		err := worker.Connect(context.Background())
		m.For(t, "err").Assert(err.Error(),
			m.Equal("ParseAddr(\"\"): unable to parse IP"))
	})

	t.Run("aws error", func(t *testing.T) {
		expectedErr := errors.New("something bad happened")

		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, expectedErr)

		worker := &AWSWorker{
			logger: logger,
			client: mAWSClient,
			id:     "id",
			port:   port,
		}

		err := worker.Connect(context.Background())
		m.For(t, "err").Assert(err, m.Equal(expectedErr))
	})

	t.Run("connection failure", func(t *testing.T) {
		mAWSClient := NewMockAWSWorkerEC2Client(ctrl)
		mAWSClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil)

		worker := &AWSWorker{
			logger: logger,
			client: mAWSClient,
			id:     "id",
			port:   port + 1, // <- CHANGE HERE!!!
		}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := worker.Connect(ctx)
		m.For(t, "err").Assert(err, m.Equal(context.DeadlineExceeded))
	})
}
