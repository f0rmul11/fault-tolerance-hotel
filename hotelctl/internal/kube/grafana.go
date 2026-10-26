package kube

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnsureGrafanaCredentials creates the initial local admin password once.
// Existing credentials survive repeated deploys and are never printed or overwritten.
func (c *Client) EnsureGrafanaCredentials(ctx context.Context) error {
	secrets := c.Typed.CoreV1().Secrets("hotelflow")
	secret, err := secrets.Get(ctx, "grafana-admin", metav1.GetOptions{})
	if err == nil {
		if len(secret.Data["admin-user"]) == 0 || len(secret.Data["admin-password"]) == 0 {
			return fmt.Errorf("grafana-admin: отсутствуют admin-user или admin-password")
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("read Grafana credentials: %w", err)
	}
	password := make([]byte, 24)
	if _, err := rand.Read(password); err != nil {
		return fmt.Errorf("generate Grafana password: %w", err)
	}
	_, err = secrets.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grafana-admin",
			Namespace: "hotelflow",
			Labels:    map[string]string{"app.kubernetes.io/part-of": "hotelflow"},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"admin-user":     []byte("admin"),
			"admin-password": []byte(base64.RawURLEncoding.EncodeToString(password)),
		},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create Grafana credentials: %w", err)
	}
	return nil
}
