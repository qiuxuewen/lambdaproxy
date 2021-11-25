package main

import (
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/awserr"
    "github.com/aws/aws-sdk-go/aws/session"
    "github.com/aws/aws-sdk-go/service/iam"
    "github.com/aws/aws-sdk-go/service/lambda"
)

const (
    _LambdaHandler     = "main"
    _LambdaRuntime     = "go1.x"
    _LambdaZipLocation = "bin/lambda.zip"
)

type AwsLambda struct {
    AwsSession_       *session.Session
    Name_             string
    IamRole_          string
    Regions_          []string
    InvokeNum_        int64
    LambdaTimeout_    int64
    LambdaMemorySize_ int64
    Mutex_            sync.RWMutex
    RoleArn_          string
}

func NewAwsLambda(name string, iam_role string, regions []string, labmda_timeout int64, lambda_mem_size int64) (*AwsLambda, error) {
    sess, err := session.NewSession(aws.NewConfig())
    if err != nil {
        return nil, fmt.Errorf("session.NewSession err %v", err)
    }
    if _, err = sess.Config.Credentials.Get(); err != nil {
        return nil, fmt.Errorf("sess.Config.Credentials.Get() err %v", err)
    }

    awsIAM := iam.New(sess, aws.NewConfig())
    roleInfo, err := awsIAM.GetRole(&iam.GetRoleInput{
        RoleName: aws.String(iam_role),
    })
    if err != nil {
        return nil, fmt.Errorf("Could not find IAM role " + iam_role)
    }

    var awsLambda = new(AwsLambda)
    awsLambda.Name_ = name
    awsLambda.IamRole_ = iam_role
    awsLambda.Regions_ = regions
    awsLambda.InvokeNum_ = 0
    awsLambda.LambdaTimeout_ = labmda_timeout
    awsLambda.LambdaMemorySize_ = lambda_mem_size
    awsLambda.RoleArn_ = *roleInfo.Role.Arn

    awsLambda.AwsSession_ = sess

    _ = awsLambda.Setup()

    return awsLambda, nil
}

func (self *AwsLambda) Setup() error {
    lambdaZipData, err := Asset(_LambdaZipLocation)
    if err != nil {
        return fmt.Errorf("Could not read ZIP file: " + _LambdaZipLocation)
    }

    for _, region := range self.Regions_ {
        err = self.DoSetup(region, lambdaZipData)
        if err != nil {
            return fmt.Errorf("Could not setup Lambda function in region " + region)
        }
    }
    return nil
}

func (self *AwsLambda) Invoke(payload []byte) error {
    region := self.Regions_[self.InvokeNum_%int64(len(self.Regions_))]

    self.Mutex_.Lock()
    lambdaZipData, err := Asset(_LambdaZipLocation)
    if err == nil {
        err = self.DoSetup(region, lambdaZipData)
        if err != nil {
            log.Fatalf("lambda invoke failed: %v", err)
        }
    }
    self.InvokeNum_++
    lamdaHandler := lambda.New(self.AwsSession_, &aws.Config{Region: aws.String(region)})
    log.Printf("Waiting lambda ready")
    time.Sleep(10 * time.Second)
    self.Mutex_.Unlock()

    _, err = lamdaHandler.Invoke(&lambda.InvokeInput{
        FunctionName: aws.String(self.Name_),
        Payload:      payload,
    })

    if err != nil {
        log.Fatalf("lambda invoke failed: %v", err)
        return err
    }
    return nil
}

func (self *AwsLambda) DoSetup(region string, lambdaZipData []byte) error {
    lamdaHandler := lambda.New(self.AwsSession_, &aws.Config{Region: aws.String(region)})
    log.Printf("Setting up Lambda function in name=%s, region=%s, invoke_num=%d.", self.Name_, region, self.InvokeNum_)
    exists, err := self.Exists(lamdaHandler, self.Name_)
    if err != nil {
        return err
    }

    if exists {
        err := self.Delete(lamdaHandler)
        if err != nil {
            return err
        }
    }

    return self.Create(lamdaHandler, lambdaZipData)
}

func (self *AwsLambda) Delete(lamdaHandler *lambda.Lambda) error {
    _, err := lamdaHandler.DeleteFunction(&lambda.DeleteFunctionInput{
        FunctionName: aws.String(self.Name_),
    })
    if err != nil {
        return err
    }
    return nil
}

func (self *AwsLambda) Create(lamdaHandler *lambda.Lambda, payload []byte) error {
    _, err := lamdaHandler.CreateFunction(&lambda.CreateFunctionInput{
        Code: &lambda.FunctionCode{
            ZipFile: payload,
        },
        FunctionName: aws.String(self.Name_),
        Handler:      aws.String(_LambdaHandler),
        Role:         aws.String(self.RoleArn_),
        Runtime:      aws.String(_LambdaRuntime),
        MemorySize:   aws.Int64(self.LambdaMemorySize_),
        Publish:      aws.Bool(true),
        Timeout:      aws.Int64(self.LambdaTimeout_),
    })
    if err != nil {
        if awsErr, ok := err.(awserr.Error); ok {
            if awsErr.Code() == "InvalidParameterValueException" {
                time.Sleep(time.Second)
                return self.Create(lamdaHandler, payload)
            }
        }
        return err
    }
    return nil
}

func (self *AwsLambda) Exists(lamdaHandler *lambda.Lambda, name string) (bool, error) {
    _, err := lamdaHandler.GetFunction(&lambda.GetFunctionInput{
        FunctionName: aws.String(name),
    })

    if err != nil {
        if awsErr, ok := err.(awserr.Error); ok {
            if awsErr.Code() == "ResourceNotFoundException" {
                return false, nil
            }
        }
        return false, err
    }

    return true, nil
}
