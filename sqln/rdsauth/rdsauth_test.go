package rdsauth

import (
	"context"
	"strings"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

func TestPassword_SignsToken(t *testing.T) {
	cfg := awssdk.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("AKID", "secret", "")}
	token, err := Password(cfg, "db.example.rds.amazonaws.com:5432", "app")(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"db.example.rds.amazonaws.com:5432?", "Action=connect", "DBUser=app", "X-Amz-Signature="} {
		if !strings.Contains(token, want) {
			t.Errorf("token %q lacks %q", token, want)
		}
	}
}

func TestPassword_RequiresCredentials(t *testing.T) {
	if _, err := Password(awssdk.Config{Region: "us-east-1"}, "db:5432", "app")(context.Background()); err == nil {
		t.Error("missing credentials must fail")
	}
}
