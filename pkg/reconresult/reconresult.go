package reconresult

import (
	"fmt"
	"github.com/ktsstudio/mirrors/api/v1alpha2"
	"time"
)

const DefaultRequeueAfter = 15 * time.Second

type ReconcileResult struct {
	Message      string
	RequeueAfter time.Duration
	Status       v1alpha2.MirrorStatus
	EventType    string
	EventReason  string
}

func (e *ReconcileResult) Error() string {
	return e.Message
}

func Fmt(format string, args ...interface{}) *ReconcileResult {
	return &ReconcileResult{
		Message:      fmt.Sprintf(format, args...),
		RequeueAfter: DefaultRequeueAfter,
	}
}
