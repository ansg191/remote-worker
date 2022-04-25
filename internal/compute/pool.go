package compute

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"go.uber.org/zap"
)

type InstancePool interface {
	io.Closer

	GetAvailableInstance(ctx context.Context) (*Instance, error)
	ReturnInstance(instance *Instance)

	getInstanceConfiguration() *ec2.RunInstancesInput
}

type Pool struct {
	logger *zap.Logger

	cfg aws.Config

	mtx                sync.Mutex  // Mutex for below slices
	allInstances       []*Instance // All instances active in pool
	availableInstances []*Instance // All instances available in pool
	deletionInstances  []*Instance // All instances marked for deletion in pool

	destroyCh chan *Instance // Channel to destroy an instance
	availCh   chan *Instance // Channel to make an instance available
	stopCh    chan bool      // Channel to stop pool management goroutine
}

func NewPool(config aws.Config, logger *zap.Logger) InstancePool {
	pool := &Pool{
		logger:    logger,
		cfg:       config,
		destroyCh: make(chan *Instance),
		availCh:   make(chan *Instance),
		stopCh:    make(chan bool),
	}

	go pool.run()

	return pool
}

func (p *Pool) run() {
	for {
		select {
		case instance := <-p.availCh:
			p.logger.Debug("Instance released back to pool", zap.String("instanceId", instance.Id))
			go func() {
				p.mtx.Lock()
				defer p.mtx.Unlock()
				if find(p.deletionInstances, instance) < 0 {
					p.availableInstances = append(p.availableInstances, instance)
				} else {
					// Delete Instance
					err := instance.Destroy(context.Background())
					if err != nil {
						fmt.Println(err)
					}

					p.allInstances = removeItem(p.allInstances, instance)

					p.logger.Debug("Instance destroyed", zap.String("instanceId", instance.Id))
				}
			}()
		case instance := <-p.destroyCh:
			p.logger.Debug("Instance sent for destruction", zap.String("instanceID", instance.Id))
			go func() {
				p.mtx.Lock()
				defer p.mtx.Unlock()

				instance.delTimer.Stop()

				if find(p.availableInstances, instance) < 0 {
					// In-use
					p.deletionInstances = append(p.deletionInstances, instance)
					p.logger.Debug("Instance marked for deletion", zap.String("instanceID", instance.Id))
				} else {
					// Delete instance
					p.availableInstances = removeItem(p.availableInstances, instance)

					err := instance.Destroy(context.Background())
					if err != nil {
						fmt.Println(err)
					}

					p.allInstances = removeItem(p.allInstances, instance)

					p.logger.Debug("Instance destroyed", zap.String("instanceId", instance.Id))
				}
			}()
		case <-p.stopCh:
			p.logger.Info("Pool management loop stopping")
			return
		}
	}
}

func (p *Pool) Close() (err error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.logger.Info("Pool closing", zap.Int("openInstances", len(p.allInstances)))
	for _, instance := range p.allInstances {
		instance.delTimer.Stop()
		err = instance.Destroy(context.Background())
		_, _ = fmt.Fprintln(os.Stderr, err)
	}
	p.stopCh <- true
	p.logger.Info("Pool closed")
	return
}

func (p *Pool) GetAvailableInstance(ctx context.Context) (*Instance, error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if len(p.availableInstances) == 0 {
		p.logger.Debug("No instance available in pool. Creating new...")
		instance, err := newInstance(ctx, p.cfg, p)
		if err != nil {
			return nil, err
		}

		p.allInstances = append(p.allInstances, instance)

		// Schedule deletion
		timer := time.AfterFunc(1*time.Minute, func() {
			p.destroyCh <- instance
		})

		instance.delTimer = timer

		p.logger.Debug("Instance created", zap.String("instanceId", instance.Id))

		return instance, err
	} else {
		var instance *Instance
		instance, p.availableInstances = p.availableInstances[0], p.availableInstances[1:]
		p.logger.Debug("Pool returning existing instance", zap.String("instanceId", instance.Id))
		return instance, nil
	}
}

func (p *Pool) ReturnInstance(instance *Instance) {
	p.availCh <- instance
}

func (p *Pool) getInstanceConfiguration() *ec2.RunInstancesInput {
	return &ec2.RunInstancesInput{
		MinCount:           aws.Int32(1),
		MaxCount:           aws.Int32(1),
		IamInstanceProfile: nil,
		ImageId:            aws.String("ami-066b217cd356469cd"),
		InstanceMarketOptions: &types.InstanceMarketOptionsRequest{
			MarketType:  types.MarketTypeSpot,
			SpotOptions: nil,
		},
		InstanceType:     "g4dn.xlarge",
		KeyName:          aws.String("us-west-ec2-keys"),
		SecurityGroupIds: []string{"sg-09b52430796d9b9c5"},
		UserData:         nil,
	}
}
