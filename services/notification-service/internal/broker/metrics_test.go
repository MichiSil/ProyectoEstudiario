package broker

import (
	"errors"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/consumer"
)

func TestResultadoDeCadaMensaje(t *testing.T) {
	for _, c := range []struct {
		action consumer.Action
		err    error
		want   string
	}{
		{consumer.Ack, nil, ResultProcessed},
		{consumer.Retry, nil, ResultRetry},
		{consumer.DeadLetter, nil, ResultDLQ},
		{consumer.DeadLetter, errors.New("canal cerrado"), ResultRequeued},
	} {
		if got := resultOf(c.action, c.err); got != c.want {
			t.Errorf("resultOf(%v, %v) = %q, se esperaba %q", c.action, c.err, got, c.want)
		}
	}
}
