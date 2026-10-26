package kube

import (
	"bytes"
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGrafanaCredentialsSurviveRepeatedDeploys(t *testing.T) {
	client := &Client{Typed: fake.NewSimpleClientset()}
	ctx := context.Background()
	if err := client.EnsureGrafanaCredentials(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := client.Typed.CoreV1().Secrets("hotelflow").Get(ctx, "grafana-admin", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Data["admin-password"]) < 24 {
		t.Fatal("missing generated password")
	}
	if err := client.EnsureGrafanaCredentials(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := client.Typed.CoreV1().Secrets("hotelflow").Get(ctx, "grafana-admin", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Data["admin-password"], after.Data["admin-password"]) {
		t.Fatal("redeploy rotated existing credentials")
	}
}
