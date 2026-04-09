package cli

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func (a *appRuntime) Shutdown(ctx context.Context) error {
	var shutdownErr error
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	if a.webServer != nil {
		if err := a.webServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			shutdownErr = err
		}
	}
	return shutdownErr
}

func shutdownRuntime(app *appRuntime) error {
	if app == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return app.Shutdown(ctx)
}
