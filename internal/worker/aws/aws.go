package aws

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"net/netip"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"golang.anshulg.com/popcorntime/go_encoder/api/proto"
	"golang.anshulg.com/popcorntime/go_encoder/internal/compute"
)

//go:embed userdata.sh
var userData []byte

var DefaultAWSInstanceParams = &ec2.RunInstancesInput{
	MinCount:           aws.Int32(1),
	MaxCount:           aws.Int32(1),
	IamInstanceProfile: nil,
	ImageId:            aws.String("ami-0892d3c7ee96c0bf7"),
	InstanceMarketOptions: &types.InstanceMarketOptionsRequest{
		MarketType:  types.MarketTypeSpot,
		SpotOptions: nil,
	},
	InstanceType:     "g4dn.xlarge",
	KeyName:          aws.String("us-west-ec2-keys"),
	SecurityGroupIds: []string{"sg-09b52430796d9b9c5"},
	UserData:         aws.String(base64.StdEncoding.EncodeToString(userData)),
}

type AWSWorkerEC2Client interface {
	RunInstances(ctx context.Context, params *ec2.RunInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeInstanceStatus(ctx context.Context, params *ec2.DescribeInstanceStatusInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstanceStatusOutput, error)
	TerminateInstances(ctx context.Context, params *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
}

type AWSWorker struct {
	logger *zap.Logger
	client AWSWorkerEC2Client

	id   string
	port uint16

	addr netip.AddrPort

	conn   *grpc.ClientConn
	worker proto.WorkerServiceClient
	job    proto.JobServiceClient

	closed bool
}

func (w *AWSWorker) Close() error {
	if w.closed {
		return compute.ErrClosed
	}

	w.closed = true

	if w.conn != nil {
		err := w.conn.Close()
		w.conn = nil
		if err != nil {
			w.logger.Error("error closing grpc connection")
		}
	}

	_, err := w.client.TerminateInstances(context.Background(), &ec2.TerminateInstancesInput{
		InstanceIds: []string{w.id},
	})
	return err
}

func (w *AWSWorker) Equals(other compute.Worker) bool {
	switch v := other.(type) {
	case *AWSWorker:
		return w.id == v.id
	default:
		return false
	}
}

func (w *AWSWorker) getIP(ctx context.Context) (netip.Addr, error) {
	instances, err := w.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{w.id},
	})
	if err != nil {
		return netip.IPv4Unspecified(), err
	}

	if len(instances.Reservations) < 1 {
		return netip.IPv4Unspecified(), errors.New("instance not found")
	}
	if len(instances.Reservations[0].Instances) < 1 {
		return netip.IPv4Unspecified(), errors.New("instance not found")
	}

	instance := instances.Reservations[0].Instances[0]

	return netip.ParseAddr(aws.ToString(instance.PublicIpAddress))
}

func (w *AWSWorker) Connect(ctx context.Context) (err error) {
	if w.closed {
		return compute.ErrClosed
	}
	if w.conn != nil {
		_ = w.conn.Close()
	}

	ip, err := w.getIP(ctx)
	if err != nil {
		return err
	}

	w.addr = netip.AddrPortFrom(ip, w.port)

	w.conn, err = grpc.DialContext(ctx, w.addr.String(),
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}

	w.worker = proto.NewWorkerServiceClient(w.conn)
	w.job = proto.NewJobServiceClient(w.conn)

	return nil
}

func (w *AWSWorker) Worker() proto.WorkerServiceClient {
	return w.worker
}

func (w *AWSWorker) Job() proto.JobServiceClient {
	return w.job
}

func (w *AWSWorker) getInstanceStatus(ctx context.Context) (types.InstanceStateName, error) {
	statuses, err := w.client.DescribeInstanceStatus(ctx, &ec2.DescribeInstanceStatusInput{
		InstanceIds: []string{w.id},
	})
	if err != nil {
		return types.InstanceStateNamePending, err
	}

	if len(statuses.InstanceStatuses) < 1 {
		return types.InstanceStateNamePending, nil
	}

	status := statuses.InstanceStatuses[0]

	if status.InstanceState != nil {
		return status.InstanceState.Name, nil
	}
	return types.InstanceStateNamePending, nil
}

func (w *AWSWorker) IsReady(ctx context.Context, opts ...compute.ReadyOptionsFunc) (bool, error) {
	if w.closed {
		return false, compute.ErrClosed
	}

	status, err := w.getInstanceStatus(ctx)
	if err != nil {
		return false, err
	}

	if status != types.InstanceStateNameRunning {
		return false, nil
	}

	options := &compute.ReadyOptions{
		ConnTimeout: 10 * time.Second,
	}

	for _, opt := range opts {
		opt(options)
	}

	connCtx, cancel := context.WithTimeout(ctx, options.ConnTimeout)
	defer cancel()

	err = w.Connect(connCtx)

	if err == context.DeadlineExceeded {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func (w *AWSWorker) IsReadyChan(ctx context.Context, opts ...compute.ReadyOptionsFunc) <-chan error {
	ch := make(chan error)

	options := &compute.ReadyOptions{
		TickerInterval: 15 * time.Second,
		ConnTimeout:    10 * time.Second,
	}

	for _, opt := range opts {
		opt(options)
	}

	go func() {
		ticker := time.NewTicker(options.TickerInterval)
		for {
			select {
			case <-ctx.Done():
				ch <- ctx.Err()
				ticker.Stop()
				return
			case <-ticker.C:
				isReady, err := w.IsReady(ctx, opts...)
				if err != nil {
					if err.Error() == "instance not found" {
						continue
					}
					ch <- err
					ticker.Stop()
					return
				}

				if isReady {
					ch <- nil
					ticker.Stop()
					return
				}
			}
		}
	}()

	return ch
}

type AWSWorkerFactory struct {
	logger *zap.Logger
	client AWSWorkerEC2Client
	params *ec2.RunInstancesInput
	port   uint16
}

func NewAWSWorkerFactory(logger *zap.Logger, client AWSWorkerEC2Client, input *ec2.RunInstancesInput, port uint16) compute.WorkerFactory {
	return &AWSWorkerFactory{
		logger: logger,
		client: client,
		params: input,
		port:   port,
	}
}

func (f *AWSWorkerFactory) Create(ctx context.Context) (compute.Worker, error) {
	instances, err := f.client.RunInstances(ctx, f.params)
	if err != nil {
		return nil, err
	}

	if len(instances.Instances) < 1 {
		return nil, errors.New("no instances found")
	}

	instance := instances.Instances[0]

	return &AWSWorker{
		logger: f.logger,
		client: f.client,
		id:     aws.ToString(instance.InstanceId),
		port:   f.port,
	}, nil
}
