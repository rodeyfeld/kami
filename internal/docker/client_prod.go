//go:build !dev

package docker

import (
	"context"
	"errors"
)

type Client struct{}

func NewClient() (*Client, error) { return nil, errors.New("docker not available in production") }
func IsAvailable() bool           { return false }
func (c *Client) GetContainerStatus(context.Context) (*ContainerStatus, error) {
	return nil, errors.New("docker not available in production")
}
