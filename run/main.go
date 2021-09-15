package main

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
)

func main() {
	svc := ecs.New(session.New())

	input := &ecs.RunTaskInput{
		// aws ecs list-clusters
		Cluster: aws.String("EcsRunTaskStack-EcsCluster97242B84-J5ZmLs75HupH"),
		// aws ecs list-task-definitions
		TaskDefinition: aws.String("EcsRunTaskStackhelloTask4FF92611"),
		NetworkConfiguration: &ecs.NetworkConfiguration{
			// aws ec2 describe-vpcs
			// aws ec2 describe-subnets --vpc=vpc-0a3d16b68fab39248
			AwsvpcConfiguration: &ecs.AwsVpcConfiguration{
				Subnets: aws.StringSlice([]string{
					// Make sure you use one that can access the internet
					// to pull the image, or use a VPC endpoint and ECR registry.
					"subnet-08b59916629d95c49",
				}),
				AssignPublicIp: aws.String("ENABLED"),
			},
		},
		LaunchType: aws.String(ecs.LaunchTypeFargate),
	}

	result, err := svc.RunTask(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case ecs.ErrCodeServerException:
				fmt.Println(ecs.ErrCodeServerException, aerr.Error())
			case ecs.ErrCodeClientException:
				fmt.Println(ecs.ErrCodeClientException, aerr.Error())
			case ecs.ErrCodeInvalidParameterException:
				fmt.Println(ecs.ErrCodeInvalidParameterException, aerr.Error())
			case ecs.ErrCodeClusterNotFoundException:
				fmt.Println(ecs.ErrCodeClusterNotFoundException, aerr.Error())
			case ecs.ErrCodeUnsupportedFeatureException:
				fmt.Println(ecs.ErrCodeUnsupportedFeatureException, aerr.Error())
			case ecs.ErrCodePlatformUnknownException:
				fmt.Println(ecs.ErrCodePlatformUnknownException, aerr.Error())
			case ecs.ErrCodePlatformTaskDefinitionIncompatibilityException:
				fmt.Println(ecs.ErrCodePlatformTaskDefinitionIncompatibilityException, aerr.Error())
			case ecs.ErrCodeAccessDeniedException:
				fmt.Println(ecs.ErrCodeAccessDeniedException, aerr.Error())
			case ecs.ErrCodeBlockedException:
				fmt.Println(ecs.ErrCodeBlockedException, aerr.Error())
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			// Print the error, cast err to awserr.Error to get the Code and
			// Message from an error.
			fmt.Println(err.Error())
		}
		return
	}

	fmt.Println(result)
}
