package main

import (
	"strings"

	"github.com/aws/aws-cdk-go/awscdk"
	"github.com/aws/aws-cdk-go/awscdk/awsec2"
	"github.com/aws/aws-cdk-go/awscdk/awsecs"
	"github.com/aws/aws-cdk-go/awscdk/awsiam"
	"github.com/aws/aws-cdk-go/awscdk/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/awslambdaeventsources"
	"github.com/aws/aws-cdk-go/awscdk/awslambdago"
	"github.com/aws/aws-cdk-go/awscdk/awss3"
	"github.com/aws/constructs-go/constructs/v3"
	"github.com/aws/jsii-runtime-go"
)

type EcsRunTaskStackProps struct {
	awscdk.StackProps
}

func NewEcsRunTaskStack(scope constructs.Construct, id string, props *EcsRunTaskStackProps) awscdk.Stack {
	var sprops awscdk.StackProps
	if props != nil {
		sprops = props.StackProps
	}
	stack := awscdk.NewStack(scope, &id, &sprops)

	// Create a bucket that, when something is added to it, it causes the Lambda function to fire, which starts a container running.
	sourceBucket := awss3.NewBucket(stack, jsii.String("sourceBucket"), &awss3.BucketProps{})
	sourceBucket.DisallowPublicAccess()

	// Create a VPC to run tasks in.
	vpc := awsec2.NewVpc(stack, jsii.String("taskVpc"), &awsec2.VpcProps{
		// If you're setting up NAT gateways, you might want to drop to 2 to save a few pounds.
		MaxAzs: jsii.Number(2),
		// If NatGateways are available, we can host in any subnet.
		// But they're a waste of money for this.
		// I'll host them in the public subnet instead.
		NatGateways: jsii.Number(0),
	})

	// Create the cluster.
	cluster := awsecs.NewCluster(stack, jsii.String("ecsCluster"), &awsecs.ClusterProps{
		Vpc: vpc,
	})

	// The task needs two roles.
	//   1. A task execution role (ter) which is used to start the task, and needs to load the containers from ECR etc.
	//   2. A task role (tr) which is used by the container when it's executing to access AWS resources.

	// Task execution role.
	// See https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task_execution_IAM_role.html
	// While there's a managed role that could be used, that CDK type doesn't have the handy GrantPassRole helper on it.
	ter := awsiam.NewRole(stack, jsii.String("taskExecutionRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("ecs-tasks.amazonaws.com"), &awsiam.ServicePrincipalOpts{}),
	})
	ter.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Actions:   jsii.Strings("ecr:BatchCheckLayerAvailability", "ecr:GetDownloadUrlForLayer", "ecr:BatchGetImage", "logs:CreateLogStream", "logs:PutLogEvents", "ecr:GetAuthorizationToken"),
		Resources: jsii.Strings("*"),
	}))

	// Task role, which needs to write to CloudWatch and read from the bucket.
	// The Task Role needs access to the bucket to receive events.
	tr := awsiam.NewRole(stack, jsii.String("taskRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("ecs-tasks.amazonaws.com"), &awsiam.ServicePrincipalOpts{}),
	})
	tr.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Actions:   jsii.Strings("logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"),
		Resources: jsii.Strings("*"),
	}))
	sourceBucket.GrantRead(tr, nil)

	// Define the task.
	td := awsecs.NewFargateTaskDefinition(stack, jsii.String("taskDefinition"), &awsecs.FargateTaskDefinitionProps{
		MemoryLimitMiB: jsii.Number(512),
		Cpu:            jsii.Number(256),
		ExecutionRole:  ter,
		TaskRole:       tr,
	})
	taskContainer := td.AddContainer(jsii.String("taskContainer"), &awsecs.ContainerDefinitionOptions{
		// Build and use the Dockerfile that's in the `../task` directory.
		Image: awsecs.AssetImage_FromAsset(jsii.String("../task"), &awsecs.AssetImageProps{}),
		Logging: awsecs.LogDriver_AwsLogs(&awsecs.AwsLogDriverProps{
			StreamPrefix: jsii.String("task"),
		}),
	})

	// The Lambda function needs a role that can start the task.
	taskStarterRole := awsiam.NewRole(stack, jsii.String("taskStarterRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("lambda.amazonaws.com"), &awsiam.ServicePrincipalOpts{}),
	})
	taskStarterRole.AddManagedPolicy(awsiam.ManagedPolicy_FromAwsManagedPolicyName(jsii.String("service-role/AWSLambdaBasicExecutionRole")))
	taskStarterRole.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Actions:   jsii.Strings("ecs:RunTask"),
		Resources: jsii.Strings(*cluster.ClusterArn(), *td.TaskDefinitionArn()),
	}))
	// Grant the Lambda permission to PassRole to enable it to tell ECS to start a task that uses the task execution role and task role.
	td.ExecutionRole().GrantPassRole(taskStarterRole)
	td.TaskRole().GrantPassRole(taskStarterRole)

	// Create a Lambda function to start the container task.
	taskStarter := awslambdago.NewGoFunction(stack, jsii.String("taskStarter"), &awslambdago.GoFunctionProps{
		Runtime: awslambda.Runtime_GO_1_X(),
		Entry:   jsii.String("../taskrunner"),
		Bundling: &awslambdago.BundlingOptions{
			GoBuildFlags: &[]*string{jsii.String(`-ldflags "-s -w"`)},
		},
		Environment: &map[string]*string{
			"CLUSTER_ARN":         cluster.ClusterArn(),
			"CONTAINER_NAME":      taskContainer.ContainerName(),
			"TASK_DEFINITION_ARN": td.TaskDefinitionArn(),
			"SUBNETS":             jsii.String(strings.Join(*getSubnetIDs(vpc.PublicSubnets()), ",")),
			"S3_BUCKET":           sourceBucket.BucketName(),
		},
		MemorySize: jsii.Number(512),
		Role:       taskStarterRole,
		Timeout:    awscdk.Duration_Millis(jsii.Number(60000)),
	})

	// Run the task starter Lambda when an object is added to the S3 bucket.
	taskStarter.AddEventSource(awslambdaeventsources.NewS3EventSource(sourceBucket, &awslambdaeventsources.S3EventSourceProps{
		Events: &[]awss3.EventType{
			awss3.EventType_OBJECT_CREATED,
		},
	}))

	return stack
}

func getSubnetIDs(subnets *[]awsec2.ISubnet) *[]string {
	sns := *subnets
	rv := make([]string, len(sns))
	for i := 0; i < len(sns); i++ {
		rv[i] = *sns[i].SubnetId()
	}
	return &rv
}

func main() {
	app := awscdk.NewApp(nil)

	NewEcsRunTaskStack(app, "EcsRunTaskStack", &EcsRunTaskStackProps{
		awscdk.StackProps{
			Env: env(),
		},
	})

	app.Synth(nil)
}

// env determines the AWS environment (account+region) in which our stack is to
// be deployed. For more information see: https://docs.aws.amazon.com/cdk/latest/guide/environments.html
func env() *awscdk.Environment {
	// If unspecified, this stack will be "environment-agnostic".
	// Account/Region-dependent features and context lookups will not work, but a
	// single synthesized template can be deployed anywhere.
	//---------------------------------------------------------------------------
	return nil

	// Uncomment if you know exactly what account and region you want to deploy
	// the stack to. This is the recommendation for production stacks.
	//---------------------------------------------------------------------------
	// return &awscdk.Environment{
	//  Account: jsii.String("123456789012"),
	//  Region:  jsii.String("us-east-1"),
	// }

	// Uncomment to specialize this stack for the AWS Account and Region that are
	// implied by the current CLI configuration. This is recommended for dev
	// stacks.
	//---------------------------------------------------------------------------
	// return &awscdk.Environment{
	//  Account: jsii.String(os.Getenv("CDK_DEFAULT_ACCOUNT")),
	//  Region:  jsii.String(os.Getenv("CDK_DEFAULT_REGION")),
	// }
}
