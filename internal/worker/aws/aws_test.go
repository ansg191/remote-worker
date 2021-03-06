package aws

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

	"github.com/ansg191/remote-worker/internal/compute"
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

func TestNewWorkerFactory(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	mClient := NewMockWorkerEC2Client(ctrl)

	factoryI := NewWorkerFactory(logger, mClient, DefaultInstanceParams, 443)

	factory := factoryI.(*WorkerFactory)

	m.For(t, "factory").For("input").
		Assert(factory.params, m.Equal(DefaultInstanceParams))
	m.For(t, "factory").For("port").Assert(factory.port, m.Equal(uint16(443)))
}

func TestWorkerFactory_Create(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("normal behavior", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			RunInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.RunInstancesOutput{
				Instances: []types.Instance{{InstanceId: aws.String("i-123456")}},
			}, nil)

		factory := NewWorkerFactory(logger, mClient, DefaultInstanceParams, 443)

		workerI, err := factory.Create(context.Background())

		m.For(t, "create err").Assert(err, m.BeNil())

		worker := workerI.(*Worker)

		m.For(t, "worker").For("client").Assert(worker.client, m.Equal(mClient))
		m.For(t, "worker").For("id").Assert(worker.id, m.Equal("i-123456"))
		m.For(t, "worker").For("port").Assert(worker.port, m.Equal(uint16(443)))
	})

	t.Run("error", func(t *testing.T) {
		expectedErr := errors.New("something bad happened")

		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			RunInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, expectedErr)

		factory := NewWorkerFactory(logger, mClient, DefaultInstanceParams, 443)

		workerI, err := factory.Create(context.Background())

		m.For(t, "create err").Assert(err, m.Equal(expectedErr))
		m.For(t, "worker").Assert(workerI, m.BeNil())
	})

	t.Run("no instance returned", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			RunInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.RunInstancesOutput{
				Instances: []types.Instance{},
			}, nil)

		factory := NewWorkerFactory(logger, mClient, DefaultInstanceParams, 443)

		workerI, err := factory.Create(context.Background())

		m.For(t, "create err").Assert(err, m.Equal(errors.New("no instances found")))
		m.For(t, "worker").Assert(workerI, m.BeNil())
	})
}

func TestWorker_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("close new worker", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			TerminateInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
		}

		err := worker.Close()
		m.For(t, "close err").Assert(err, m.BeNil())
	})

	t.Run("double close worker", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			TerminateInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
		}

		err := worker.Close()
		m.For(t, "close err").Require(err, m.BeNil())

		err = worker.Close()
		m.For(t, "close err 2").Assert(err, m.Equal(compute.ErrClosed))
	})

	t.Run("close connected worker", func(t *testing.T) {
		addr, _ := grpcServer(t)

		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			TerminateInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
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

		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			TerminateInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
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

func TestWorker_Equals(t *testing.T) {
	tests := []struct {
		name   string
		w1     *Worker
		w2     compute.Worker
		expect bool
	}{
		{
			name:   "same",
			w1:     &Worker{id: "id"},
			w2:     &Worker{id: "id"},
			expect: true,
		},
		{
			name:   "different",
			w1:     &Worker{id: "id"},
			w2:     &Worker{id: "id2"},
			expect: false,
		},
		{
			name:   "different2",
			w1:     &Worker{id: "id2"},
			w2:     &Worker{id: "id"},
			expect: false,
		},
		{
			name:   "nil",
			w1:     &Worker{id: "id"},
			w2:     nil,
			expect: false,
		},
		{
			name:   "different worker",
			w1:     &Worker{id: "id"},
			w2:     &compute.MockWorker{},
			expect: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m.For(t, "equal").Assert(test.w1.Equals(test.w2), m.Equal(test.expect))
		})
	}
}

func TestWorker_Connect(t *testing.T) {
	addr, _ := grpcServer(t)

	ip := addr.AddrPort().Addr()
	port := addr.AddrPort().Port()

	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("normal behavior", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		err := worker.Connect(context.Background())
		m.For(t, "err").Assert(err, m.BeNil())
	})

	t.Run("double connect", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil).
			Times(2)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		err := worker.Connect(context.Background())
		m.For(t, "err").Require(err, m.BeNil())

		err = worker.Connect(context.Background())
		m.For(t, "err").Assert(err, m.BeNil())
	})

	t.Run("connect to closed", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			TerminateInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		err := worker.Close()
		m.For(t, "close err").Require(err, m.BeNil())

		err = worker.Connect(context.Background())
		m.For(t, "err").Assert(err, m.Equal(compute.ErrClosed))
	})

	t.Run("missing ip", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: nil,
					}}},
				},
			}, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		err := worker.Connect(context.Background())
		m.For(t, "err").Assert(err.Error(),
			m.Equal("ParseAddr(\"\"): unable to parse IP"))
	})

	t.Run("aws error", func(t *testing.T) {
		expectedErr := errors.New("something bad happened")

		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, expectedErr)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		err := worker.Connect(context.Background())
		m.For(t, "err").Assert(err, m.Equal(expectedErr))
	})

	t.Run("connection failure", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port + 1, // <- CHANGE HERE!!!
		}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := worker.Connect(ctx)
		m.For(t, "err").Assert(err, m.Equal(context.DeadlineExceeded))
	})
}

func TestWorker_IsReady(t *testing.T) {
	addr, _ := grpcServer(t)

	ip := addr.AddrPort().Addr()
	port := addr.AddrPort().Port()

	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("normal behavior", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil)
		mClient.EXPECT().
			DescribeInstanceStatus(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstanceStatusOutput{
				InstanceStatuses: []types.InstanceStatus{{
					InstanceState: &types.InstanceState{
						Name: types.InstanceStateNameRunning,
					},
				}},
			}, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		ready, err := worker.IsReady(context.Background())
		m.For(t, "err").Assert(err, m.BeNil())
		m.For(t, "ready").Assert(ready, m.Equal(true))
	})

	t.Run("pending status", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstanceStatus(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstanceStatusOutput{
				InstanceStatuses: []types.InstanceStatus{{
					InstanceState: &types.InstanceState{
						Name: types.InstanceStateNamePending,
					},
				}},
			}, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		ready, err := worker.IsReady(context.Background())
		m.For(t, "err").Assert(err, m.BeNil())
		m.For(t, "ready").Assert(ready, m.Equal(false))
	})

	t.Run("status error", func(t *testing.T) {
		expectedErr := errors.New("something bad happened")

		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstanceStatus(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, expectedErr)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		ready, err := worker.IsReady(context.Background())
		m.For(t, "err").Assert(err, m.Equal(expectedErr))
		m.For(t, "ready").Assert(ready, m.Equal(false))
	})

	t.Run("connection timeout", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil)
		mClient.EXPECT().
			DescribeInstanceStatus(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstanceStatusOutput{
				InstanceStatuses: []types.InstanceStatus{{
					InstanceState: &types.InstanceState{
						Name: types.InstanceStateNameRunning,
					},
				}},
			}, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port + 1, // <- CHANGE HERE
		}

		ready, err := worker.IsReady(context.Background(), compute.WithConnTimeout(1*time.Millisecond))
		m.For(t, "err").Assert(err, m.BeNil())
		m.For(t, "ready").Assert(ready, m.Equal(false))
	})

	t.Run("already closed", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			TerminateInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil)

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		err := worker.Close()
		m.For(t, "close").For("err").Require(err, m.BeNil())

		ready, err := worker.IsReady(context.Background())
		m.For(t, "err").Assert(err, m.Equal(compute.ErrClosed))
		m.For(t, "ready").Assert(ready, m.Equal(false))
	})
}

func TestWorker_IsReadyChan(t *testing.T) {
	addr, _ := grpcServer(t)

	ip := addr.AddrPort().Addr()
	port := addr.AddrPort().Port()

	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	t.Cleanup(func() {
		ctrl.Finish()
	})

	t.Run("normal behavior", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil).
			AnyTimes()
		mClient.EXPECT().
			DescribeInstanceStatus(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstanceStatusOutput{
				InstanceStatuses: []types.InstanceStatus{{
					InstanceState: &types.InstanceState{
						Name: types.InstanceStateNameRunning,
					},
				}},
			}, nil).
			AnyTimes()

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		ch := worker.IsReadyChan(context.Background(),
			compute.WithTickerInterval(50*time.Millisecond),
			compute.WithConnTimeout(100*time.Millisecond))
		m.For(t, "ch err").Assert(<-ch, m.BeNil())
	})

	t.Run("double check", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil).
			AnyTimes()
		mClient.EXPECT().
			DescribeInstanceStatus(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstanceStatusOutput{
				InstanceStatuses: []types.InstanceStatus{{
					InstanceState: &types.InstanceState{
						Name: types.InstanceStateNameRunning,
					},
				}},
			}, nil).
			After(mClient.EXPECT().
				DescribeInstanceStatus(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&ec2.DescribeInstanceStatusOutput{
					InstanceStatuses: []types.InstanceStatus{{
						InstanceState: &types.InstanceState{
							Name: types.InstanceStateNamePending,
						},
					}},
				}, nil).
				Times(1)).
			AnyTimes()

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		ch := worker.IsReadyChan(context.Background(),
			compute.WithTickerInterval(50*time.Millisecond),
			compute.WithConnTimeout(100*time.Millisecond))
		m.For(t, "ch err").Assert(<-ch, m.BeNil())
	})

	t.Run("context expired", func(t *testing.T) {
		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil).
			AnyTimes()
		mClient.EXPECT().
			DescribeInstanceStatus(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstanceStatusOutput{
				InstanceStatuses: []types.InstanceStatus{{
					InstanceState: &types.InstanceState{
						Name: types.InstanceStateNamePending,
					},
				}},
			}, nil).
			AnyTimes()

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		ch := worker.IsReadyChan(ctx,
			compute.WithTickerInterval(50*time.Millisecond),
			compute.WithConnTimeout(100*time.Millisecond))
		m.For(t, "ch err").Assert(<-ch, m.Equal(context.DeadlineExceeded))
	})

	t.Run("error", func(t *testing.T) {
		expectedErr := errors.New("something bad happened")

		mClient := NewMockWorkerEC2Client(ctrl)
		mClient.EXPECT().
			DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{Instances: []types.Instance{{
						PublicIpAddress: aws.String(ip.String()),
					}}},
				},
			}, nil).
			AnyTimes()
		mClient.EXPECT().
			DescribeInstanceStatus(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, expectedErr).
			AnyTimes()

		worker := &Worker{
			logger: logger,
			client: mClient,
			id:     "id",
			port:   port,
		}

		ch := worker.IsReadyChan(context.Background(),
			compute.WithTickerInterval(50*time.Millisecond),
			compute.WithConnTimeout(100*time.Millisecond))
		m.For(t, "ch err").Assert(<-ch, m.Equal(expectedErr))
	})
}
