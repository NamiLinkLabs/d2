//go:build !js && !wasip1

package limits

import "context"

// DeadlineErr reports a passed deadline that ctx has not observed yet. On
// platforms with a preemptive scheduler the deadline timer closes Done on
// time, so there is nothing to add.
func DeadlineErr(context.Context) error {
	return nil
}
