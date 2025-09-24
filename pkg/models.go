package pkg

import (
	"context"
	"onyxia-janitor/pkg/onyxia"
)

type FailedItemType string

const (
	User    FailedItemType = "user"
	Release FailedItemType = "release"
)

type ServiceWithReleaseActionResult struct {
	FailedItems     []string
	FailedItemType  FailedItemType
	SuccessfulCount int
}

type ServiceWithReleaseAction interface {
	Process(ctx context.Context, swr onyxia.ServiceWithRelease) error
	Finish(ctx context.Context) (*ServiceWithReleaseActionResult, error)
}
