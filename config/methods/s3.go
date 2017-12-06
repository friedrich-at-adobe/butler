package methods

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type S3Method struct {
	Bucket     string                `mapstructure:"bucket" json:"bucket"`
	Manager    *string               `json:"-"`
	Region     string                `mapstructure:"region" json:"region"`
	Downloader *s3manager.Downloader `json:"-"`
}

func NewS3Method(manager *string, entry *string) (Method, error) {
	var (
		err    error
		result S3Method
	)

	if (manager != nil) && (entry != nil) {

		err = viper.UnmarshalKey(*entry, &result)
		if err != nil {
			return result, err
		}

		// We should have something for both of these
		if (result.Bucket == "") || (result.Region == "") {
			return S3Method{}, errors.New("s3 bucket or region is not defined in config")
		}
	}

	sess, err := session.NewSession(&aws.Config{Region: aws.String(result.Region)})
	if err != nil {
		return S3Method{}, errors.New("could not start s3 session")
	}

	downloader := s3manager.NewDownloader(sess)

	result.Downloader = downloader
	result.Manager = manager

	return result, err
}

func NewS3MethodWithRegionAndBucket(region string, bucket string) (Method, error) {
	var result S3Method

	sess, err := session.NewSession(&aws.Config{Region: aws.String(region)})
	if err != nil {
		return S3Method{}, errors.New("could not start s3 session")
	}
	downloader := s3manager.NewDownloader(sess)

	result.Downloader = downloader
	result.Manager = nil
	result.Region = region
	result.Bucket = bucket
	return result, err
}

func (s S3Method) Get(file string) (*Response, error) {
	var (
		response Response
	)

	tmpFile, err := ioutil.TempFile("/tmp", "s3pcmsfile")
	if err != nil {
		return &Response{}, errors.New(fmt.Sprintf("S3Method::Get(): could not create temp file err=%v", err))
	}

	log.Debugf("S3Method::Get(): going to download s3 region=%v, bucket=%v, key=%v", s.Region, s.Bucket, file)
	_, err = s.Downloader.Download(tmpFile,
		&s3.GetObjectInput{
			Bucket: aws.String(s.Bucket),
			Key:    aws.String(file),
		})
	if err != nil {
		//e := err.(awserr.RequestFailure)
		var code int
		if e, ok := err.(awserr.RequestFailure); ok {
			code = e.StatusCode()
		}
		if e, ok := err.(awserr.Error); ok {
			err = e.OrigErr()
			// actually couldn't fulfill the reqeust since the host
			// probably doesn't exist. code = 504 is probably wrong but
			// whatever... gateway timeout will have to be good enough ;)
			code = 504
		}
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		//return &Response{statusCode: e.StatusCode()}, errors.New(fmt.Sprintf("S3Method::Get(): caught error for download err=%v", err.Error()))
		return &Response{statusCode: code}, errors.New(fmt.Sprintf("S3Method::Get(): caught error for download err=%v", err.Error()))
	}

	fileData, err := ioutil.ReadFile(tmpFile.Name())
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return &Response{statusCode: 500}, errors.New(fmt.Sprintf("S3Method::Get(): caught error read file err=%v", err.Error()))
	}

	// Clean up the tmpfile
	tmpFile.Close()
	os.Remove(tmpFile.Name())

	response.statusCode = 200
	response.body = ioutil.NopCloser(bytes.NewReader(fileData))

	// Perhaps we need to do more stuff here
	return &response, nil
}