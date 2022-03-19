package silenterror

import (
	"fmt"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"
)

const DefaultRequeueAfter = 15 * time.Second

type SilentError struct {
	Message      string
	RequeueAfter time.Duration
}

func (e *SilentError) Error() string {
	return e.Message
}

func Fmt(format string, args ...interface{}) *SilentError {
	return &SilentError{
		Message:      fmt.Sprintf(format, args...),
		RequeueAfter: DefaultRequeueAfter,
	}
}

func FmtWithRequeue(requeueAfter time.Duration, format string, args ...interface{}) *SilentError {
	return &SilentError{
		Message:      fmt.Sprintf(format, args...),
		RequeueAfter: requeueAfter,
	}
}

func ToCtrlResult(logger logr.Logger, err error) (ctrl.Result, error) {
	if err != nil {
		if silentErr, ok := err.(*SilentError); ok {
			logger.Info(silentErr.Error())
			return ctrl.Result{
				RequeueAfter: silentErr.RequeueAfter,
			}, nil
		} else {
			return ctrl.Result{RequeueAfter: DefaultRequeueAfter}, err
		}
	}
	return ctrl.Result{}, nil
}
