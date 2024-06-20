package context

import "github.com/labstack/echo/v4"

func IsPublicViewerEnabled(ctx echo.Context) bool {
	publicViewerEnabled, _ := ctx.Get(PublicViewerEnabled).(bool)
	return publicViewerEnabled
}
