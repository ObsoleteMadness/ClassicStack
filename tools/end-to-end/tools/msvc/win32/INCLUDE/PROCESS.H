/***
*process.h - definition and declarations for process control functions
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This file defines the modeflag values for spawnxx calls.
*	Only P_WAIT and P_OVERLAY are currently implemented on MS-DOS.
*	Also contains the function argument declarations for all
*	process control related routines.
*
****/

#ifndef _INC_PROCESS

#ifndef _POSIX_

#ifdef __cplusplus
extern "C" {
#endif


#ifndef _CRTIMP
#ifdef _DLL
#define _CRTIMP __declspec(dllimport)
#else
#define _CRTIMP
#endif
#endif


/* modeflag values for _spawnxx routines */

#ifndef _MT
extern int _p_overlay;
#endif

#define _P_WAIT 	0
#define _P_NOWAIT	1
#ifdef	_MT
#define _P_OVERLAY	2
#else
#define _P_OVERLAY	_p_overlay
#endif
#define _OLD_P_OVERLAY	2
#define _P_NOWAITO	3
#define _P_DETACH	4


/* Action codes for _cwait(). The action code argument to _cwait is ignored
   on Win32 though it is accepted for compatibilty with OS/2 */

#define _WAIT_CHILD	 0
#define _WAIT_GRANDCHILD 1


/* function prototypes */

#ifdef _MT
_CRTIMP unsigned long  __cdecl _beginthread (void (__cdecl *) (void *),
	unsigned, void *);
_CRTIMP void __cdecl _endthread(void);
#endif
_CRTIMP void __cdecl abort(void);
_CRTIMP void __cdecl _cexit(void);
_CRTIMP void __cdecl _c_exit(void);
_CRTIMP int __cdecl _cwait(int *, int, int);
_CRTIMP int __cdecl _execl(const char *, const char *, ...);
_CRTIMP int __cdecl _execle(const char *, const char *, ...);
_CRTIMP int __cdecl _execlp(const char *, const char *, ...);
_CRTIMP int __cdecl _execlpe(const char *, const char *, ...);
_CRTIMP int __cdecl _execv(const char *, const char * const *);
_CRTIMP int __cdecl _execve(const char *, const char * const *, const char * const *);
_CRTIMP int __cdecl _execvp(const char *, const char * const *);
_CRTIMP int __cdecl _execvpe(const char *, const char * const *, const char * const *);
_CRTIMP void __cdecl exit(int);
_CRTIMP void __cdecl _exit(int);
_CRTIMP int __cdecl _getpid(void);
_CRTIMP int __cdecl _spawnl(int, const char *, const char *, ...);
_CRTIMP int __cdecl _spawnle(int, const char *, const char *, ...);
_CRTIMP int __cdecl _spawnlp(int, const char *, const char *, ...);
_CRTIMP int __cdecl _spawnlpe(int, const char *, const char *, ...);
_CRTIMP int __cdecl _spawnv(int, const char *, const char * const *);
_CRTIMP int __cdecl _spawnve(int, const char *, const char * const *,
	const char * const *);
_CRTIMP int __cdecl _spawnvp(int, const char *, const char * const *);
_CRTIMP int __cdecl _spawnvpe(int, const char *, const char * const *,
	const char * const *);
_CRTIMP int __cdecl system(const char *);

#ifdef _DECL_DLLMAIN
/*
 * Declare DLL notification (initialization/termination) routines
 *	The preferred method is for the user to provide DllMain() which will
 *	be called automatically by the DLL entry point defined by the C run-
 *	time library code.  If the user wants to define the DLL entry point
 *	routine, the user's entry point must call _CRT_INIT on all types of
 *	notifications, as the very first thing on attach notifications and
 *	as the very last thing on detach notifications.
 */
#ifdef _WINDOWS_	/* Use types from WINDOWS.H */
BOOL WINAPI DllMain(HANDLE, DWORD, LPVOID);
BOOL WINAPI _CRT_INIT(HANDLE, DWORD, LPVOID);
#else
int __stdcall DllMain(void *, unsigned, void *);
int __stdcall _CRT_INIT(void *, unsigned, void *);
#endif /* _WINDOWS_ */
#endif

#if !__STDC__
/* Non-ANSI names for compatibility */

#define P_WAIT		_P_WAIT
#define P_NOWAIT	_P_NOWAIT
#define P_OVERLAY	_P_OVERLAY
#define OLD_P_OVERLAY	_OLD_P_OVERLAY
#define P_NOWAITO	_P_NOWAITO
#define P_DETACH	_P_DETACH

#define WAIT_CHILD	_WAIT_CHILD
#define WAIT_GRANDCHILD _WAIT_GRANDCHILD

_CRTIMP int __cdecl cwait(int *, int, int);
_CRTIMP int __cdecl execl(const char *, const char *, ...);
_CRTIMP int __cdecl execle(const char *, const char *, ...);
_CRTIMP int __cdecl execlp(const char *, const char *, ...);
_CRTIMP int __cdecl execlpe(const char *, const char *, ...);
_CRTIMP int __cdecl execv(const char *, const char * const *);
_CRTIMP int __cdecl execve(const char *, const char * const *, const char * const *);
_CRTIMP int __cdecl execvp(const char *, const char * const *);
_CRTIMP int __cdecl execvpe(const char *, const char * const *, const char * const *);
_CRTIMP int __cdecl getpid(void);
_CRTIMP int __cdecl spawnl(int, const char *, const char *, ...);
_CRTIMP int __cdecl spawnle(int, const char *, const char *, ...);
_CRTIMP int __cdecl spawnlp(int, const char *, const char *, ...);
_CRTIMP int __cdecl spawnlpe(int, const char *, const char *, ...);
_CRTIMP int __cdecl spawnv(int, const char *, const char * const *);
_CRTIMP int __cdecl spawnve(int, const char *, const char * const *,
	const char * const *);
_CRTIMP int __cdecl spawnvp(int, const char *, const char * const *);
_CRTIMP int __cdecl spawnvpe(int, const char *, const char * const *,
	const char * const *);
#endif

#ifdef __cplusplus
}
#endif

#endif	/* _POSIX_ */

#define _INC_PROCESS
#endif	/* _INC_PROCESS */
