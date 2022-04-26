package compute_test

//go:generate mockgen -source=pool.go -destination=pool_mock.go -package compute
//go:generate mockgen -source=worker.go -destination=worker_mock.go -package compute
//go:generate mockgen -source=aws.go -destination=aws_mock.go -package compute
