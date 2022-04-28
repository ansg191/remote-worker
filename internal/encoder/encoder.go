package encoder

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/xfrr/goffmpeg/models"
	"github.com/xfrr/goffmpeg/transcoder"
	"go.uber.org/zap"

	"github.com/ansg191/go_encoder/api/proto"
)

type EncodeJob interface {
	SetSourcePath(path string) EncodeJob
	SetDestPath(path string) EncodeJob
	SetCodec(codec string) EncodeJob
	SetBitrate(bitrate string) EncodeJob

	GetStatus() <-chan *proto.JobStatus
	Start()
	Wait()
	Cancel()
}

type DefaultEncodeJob struct {
	logger *zap.Logger
	cfg    aws.Config

	tempPath string

	sourcePath     string
	sourceFilePath string
	destPath       string
	destFilePath   string

	codec   string
	bitrate string

	status chan *proto.JobStatus
	done   chan bool
	cancel chan bool
}

func NewEncodeJob(logger *zap.Logger, cfg aws.Config, tempPath string) EncodeJob {
	return &DefaultEncodeJob{
		logger:   logger,
		cfg:      cfg,
		tempPath: tempPath,
		status:   make(chan *proto.JobStatus, 1024),
		done:     make(chan bool, 1),
		cancel:   make(chan bool, 1),
	}
}

func (d *DefaultEncodeJob) SetSourcePath(path string) EncodeJob {
	d.sourcePath = path
	return d
}

func (d *DefaultEncodeJob) SetDestPath(path string) EncodeJob {
	d.destPath = path
	return d
}

func (d *DefaultEncodeJob) SetCodec(codec string) EncodeJob {
	d.codec = codec
	return d
}

func (d *DefaultEncodeJob) SetBitrate(bitrate string) EncodeJob {
	d.bitrate = bitrate
	return d
}

func (d *DefaultEncodeJob) GetStatus() <-chan *proto.JobStatus {
	return d.status
}

func (d *DefaultEncodeJob) Start() {
	defer func() {
		d.done <- true
		close(d.status)
	}()

	err := d.setup()
	if err != nil {
		d.status <- &proto.JobStatus{
			Status: proto.JobStatus_ERROR,
			Error:  err.Error(),
		}
		return
	}

	err = d.transcode()
	if err != nil {
		d.status <- &proto.JobStatus{
			Status: proto.JobStatus_ERROR,
			Error:  err.Error(),
		}
	}

	err = d.cleanup(err)
	if err != nil {
		d.status <- &proto.JobStatus{
			Status: proto.JobStatus_ERROR,
			Error:  err.Error(),
		}
		return
	}
}

func (d *DefaultEncodeJob) setup() error {
	if strings.HasPrefix(d.sourcePath, "s3://") {
		// Download file
		d.status <- &proto.JobStatus{
			Status: proto.JobStatus_DOWNLOADING,
		}

		// This is scuffed, but works for 3 letter extensions
		filePath := path.Join(d.tempPath, fmt.Sprintf("tmp.%s", d.sourcePath[len(d.sourcePath)-3:]))

		u, err := url.Parse(d.sourcePath)
		if err != nil {
			return err
		}
		d.logger.Debug("download s3 breakdown",
			zap.String("protocol", u.Scheme),
			zap.String("bucket", u.Host),
			zap.String("key", u.Path[1:]))

		file, err := os.Create(filePath)
		if err != nil {
			return err
		}
		defer func(file *os.File) {
			_ = file.Close()
		}(file)

		downloader := manager.NewDownloader(s3.NewFromConfig(d.cfg))

		_, err = downloader.Download(context.Background(), file, &s3.GetObjectInput{
			Bucket: aws.String(u.Host),
			Key:    aws.String(u.Path[1:]),
		})
		if err != nil {
			_ = os.Remove(filePath)
			return err
		}

		d.sourceFilePath = filePath
	} else {
		d.sourceFilePath = d.sourcePath
	}

	if strings.HasPrefix(d.destPath, "s3://") {
		filePath := path.Join(d.tempPath, "out.mp4")

		file, err := os.Create(filePath)
		if err != nil {
			return err
		}
		defer func(file *os.File) {
			_ = file.Close()
		}(file)

		d.destFilePath = filePath
	} else {
		d.destFilePath = d.destPath
	}

	return nil
}

func (d *DefaultEncodeJob) transcode() error {
	trans := new(transcoder.Transcoder)

	err := trans.Initialize(d.sourceFilePath, d.destFilePath)
	if err != nil {
		return err
	}

	trans.MediaFile().SetVideoCodec(d.codec)
	trans.MediaFile().SetVideoBitRate(d.bitrate)

	cmdString := fmt.Sprintf("%s %s", trans.FFmpegExec(), strings.Join(trans.GetCommand(), " "))
	d.logger.Info("running ffmpeg",
		zap.String("cmd", cmdString),
		zap.Strings("args", trans.GetCommand()),
	)

	done := trans.Run(true)

	progress := trans.Output()

	for msg := range progress {
		select {
		case <-d.cancel:
			_ = trans.Stop()
			return errors.New("job canceled")
		default:
		}

		status, err := ProgressToProto(msg)
		if err != nil {
			d.status <- &proto.JobStatus{
				Status: proto.JobStatus_ERROR,
				Error:  err.Error(),
			}
		} else {
			d.status <- status
		}
	}

	return <-done
}

func (d *DefaultEncodeJob) cleanup(err error) error {
	if d.sourcePath != d.sourceFilePath {
		defer func(name string) {
			err := os.Remove(name)
			if err != nil {
				d.logger.Error("issue deleting temporary file", zap.Error(err))
			}
		}(d.sourceFilePath)
	}
	if d.destPath != d.destFilePath {
		defer func(name string) {
			err := os.Remove(name)
			if err != nil {
				d.logger.Error("issue deleting temporary file", zap.Error(err))
			}
		}(d.destFilePath)
	}

	if err != nil {
		return nil
	}

	if strings.HasPrefix(d.destPath, "s3://") {
		d.status <- &proto.JobStatus{
			Status: proto.JobStatus_UPLOADING,
		}

		d.logger.Debug("Uploading file",
			zap.String("path", d.destFilePath),
			zap.String("location", d.destPath))

		file, err := os.Open(d.destFilePath)
		if err != nil {
			return err
		}
		defer func(file *os.File) {
			_ = file.Close()
		}(file)

		u, err := url.Parse(d.destPath)
		if err != nil {
			return err
		}
		d.logger.Debug("upload s3 breakdown",
			zap.String("protocol", u.Scheme),
			zap.String("bucket", u.Host),
			zap.String("key", u.Path[1:]))

		uploader := manager.NewUploader(s3.NewFromConfig(d.cfg))
		_, err = uploader.Upload(context.Background(), &s3.PutObjectInput{
			Bucket: aws.String(u.Host),
			Key:    aws.String(u.Path[1:]),
			Body:   file,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *DefaultEncodeJob) Wait() {
	<-d.done
}

func (d *DefaultEncodeJob) Cancel() {
	d.cancel <- true
}

func ProgressToProto(progress models.Progress) (*proto.JobStatus, error) {
	framesProcessed, err := strconv.ParseInt(progress.FramesProcessed, 10, 32)
	if err != nil {
		return nil, err
	}

	speed, err := strconv.ParseFloat(progress.Speed[:len(progress.Speed)-1], 64)
	if err != nil {
		return nil, err
	}

	return &proto.JobStatus{
		Status: proto.JobStatus_ENCODING,
		EncodeStatus: &proto.EncodeStatus{
			FramesProcessed: int32(framesProcessed),
			CurrTime:        progress.CurrentTime,
			BitRate:         progress.CurrentBitrate,
			Progress:        progress.Progress,
			Speed:           speed,
		},
	}, nil
}
