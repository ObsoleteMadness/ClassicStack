/*
Command classicstack-svc runs ClassicStack as a Windows service.

It registers with the Service Control Manager and runs the same stack as the
interactive classicstack binary, in-process, sharing the run-core in
internal/app. Subcommands:

	classicstack-svc install   -config <path>   register the service (auto-start)
	classicstack-svc uninstall                  remove the service
	classicstack-svc start                       start the registered service
	classicstack-svc stop                        stop the registered service
	classicstack-svc status                      report the service state
	classicstack-svc run        -config <path>   run under the SCM (invoked by Windows)

On non-Windows platforms this binary is a stub; use the classicstackd daemon
instead.
*/
package main
