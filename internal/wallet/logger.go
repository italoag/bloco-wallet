package wallet

import "blocowallet/pkg/logger"

// svcLogger is an optional file-based logger injected from the main
var svcLogger logger.Logger

// SetLogger allows the application to inject a file-based logger for wallet services
func SetLogger(l logger.Logger) { svcLogger = l }
