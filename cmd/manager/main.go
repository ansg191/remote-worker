package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"go.uber.org/zap"

	"golang.anshulg.com/popcorntime/go_encoder/api/proto"
	"golang.anshulg.com/popcorntime/go_encoder/internal/compute"
)

func run() error {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	defer logger.Sync()

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return err
	}

	pool := compute.NewPool(logger, compute.NewAWSWorkerFactory(logger, cfg, compute.DefaultAWSInstanceParams))
	defer pool.Close()

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
