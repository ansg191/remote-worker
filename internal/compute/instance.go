package compute

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type Instance struct {
	cfg      aws.Config
	pool     InstancePool
	delTimer *time.Timer

	Id   string
	Addr net.Addr
}

func newInstance(ctx context.Context, cfg aws.Config, pool InstancePool) (*Instance, error) {
	client := ec2.NewFromConfig(cfg)

	instances, err := client.RunInstances(ctx, pool.getInstanceConfiguration())
	if err != nil {
		return nil, err
	}

	if len(instances.Instances) < 1 {
		return nil, errors.New("no instances found")
	}

	instance := instances.Instances[0]

	addr, err := net.ResolveIPAddr("ip4", aws.ToString(instance.PublicIpAddress))
	if err != nil {
		return nil, err
	}

	return &Instance{
		cfg:  cfg,
		pool: pool,
		Id:   aws.ToString(instance.InstanceId),
		Addr: addr,
	}, nil
}

func (i *Instance) IsReady(ctx context.Context) (bool, error) {
	cli := ec2.NewFromConfig(i.cfg)

	status, err := cli.DescribeInstanceStatus(ctx, &ec2.DescribeInstanceStatusInput{
		InstanceIds: []string{i.Id},
	})
	if err != nil {
		return false, err
	}

	if len(status.InstanceStatuses) < 1 {
		return false, errors.New("no instance statuses returned")
	}

	instanceStatus := status.InstanceStatuses[0]

	if instanceStatus.InstanceState != nil && instanceStatus.InstanceStatus != nil && instanceStatus.SystemStatus != nil {
		return instanceStatus.InstanceState.Name == types.InstanceStateNameRunning &&
			instanceStatus.InstanceStatus.Status == types.SummaryStatusOk &&
			instanceStatus.SystemStatus.Status == types.SummaryStatusOk, nil
	}

	return false, nil
}

func (i *Instance) IsReadyChan(ctx context.Context) chan error {
	ch := make(chan error)

	ticker := time.NewTicker(5 * time.Second)

	go func() {
		for {
			select {
			case <-ctx.Done():
				ch <- ctx.Err()
				ticker.Stop()
				return
			case <-ticker.C:
				isReady, err := i.IsReady(ctx)
				if err != nil {
					if err.Error() == "no instance statuses returned" {
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

func (i *Instance) Refresh(ctx context.Context) error {
	client := ec2.NewFromConfig(i.cfg)

	instances, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{i.Id},
	})
	if err != nil {
		return err
	}

	if len(instances.Reservations) < 1 {
		return errors.New("instance not found")
	}
	if len(instances.Reservations[0].Instances) < 1 {
		return errors.New("instance not found")
	}

	instance := instances.Reservations[0].Instances[0]

	addr, err := net.ResolveIPAddr("ip4", aws.ToString(instance.PublicIpAddress))
	if err != nil {
		return err
	}

	i.Addr = addr

	return nil
}

func (i *Instance) Close() error {
	i.pool.ReturnInstance(i)
	return nil
}

func (i *Instance) Destroy(ctx context.Context) error {
	client := ec2.NewFromConfig(i.cfg)

	_, err := client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{i.Id},
	})
	return err
}
