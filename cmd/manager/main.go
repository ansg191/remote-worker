package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"go.uber.org/zap"

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

	pool := compute.NewPool(cfg, logger)
	defer pool.Close()

	queue := compute.NewQueue(logger, pool, 2)

	info := compute.NewWorkInfo(
		context.Background(),
		80,
		func(ctx context.Context, logger *zap.Logger, req int, instance *compute.Instance) (string, error) {
			return fmt.Sprintf("%s:%d", instance.Addr.String(), req), nil
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
