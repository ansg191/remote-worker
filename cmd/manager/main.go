package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"go.uber.org/zap"

	"golang.anshulg.com/popcorntime/go_encoder/api/proto"
	"golang.anshulg.com/popcorntime/go_encoder/internal/compute"
	"golang.anshulg.com/popcorntime/go_encoder/internal/worker/aws"
)

func run() error {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	defer func(logger *zap.Logger) {
		_ = logger.Sync()
	}(logger)

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return err
	}

	pool := compute.NewPool(logger, aws.NewAWSWorkerFactory(logger, ec2.NewFromConfig(cfg), aws.DefaultAWSInstanceParams, 443))
	defer func(pool compute.Pool) {
		_ = pool.Close()
	}(pool)

	queue := compute.NewQueue(logger, pool, 2)

	info := compute.NewWorkInfo(
		context.Background(),
		nil,
		func(ctx context.Context, logger *zap.Logger, req any, worker compute.Worker) (string, error) {
			res, err := worker.Worker().Status(ctx, &proto.WorkerStatusRequest{})
			return res.Msg, err
		},
	)

	queue.Add(info)

	queue.Wait()

	select {
	case result := <-info.Result:
		fmt.Println(result)
	case err = <-info.Err:
		fmt.Println(err)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "err: %v\n", err)
		os.Exit(1)
	}
}
