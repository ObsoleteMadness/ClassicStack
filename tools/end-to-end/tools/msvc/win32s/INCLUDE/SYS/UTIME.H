/***
*sys/utime.h - definitions/declarations for utime()
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This file defines the structure used by the utime routine to set
*	new file access and modification times.  NOTE - MS-DOS
*	does not recognize access time, so this field will
*	always be ignored and the modification time field will be
*	used to set the new time.
*
****/

#ifndef _INC_UTIME

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

#ifndef _TIME_T_DEFINED
typedef long time_t;
#define _TIME_T_DEFINED
#endif

/* define struct used by _utime() function */

#ifndef _UTIMBUF_DEFINED
struct _utimbuf {
	time_t actime;		/* access time */
	time_t modtime; 	/* modification time */
	};
#if !__STDC__
/* Non-ANSI name for compatibility */
struct utimbuf {
	time_t actime;		/* access time */
	time_t modtime; 	/* modification time */
	};
#endif

#define _UTIMBUF_DEFINED
#endif

/* function prototypes */

_CRTIMP int __cdecl _utime(char *, struct _utimbuf *);
_CRTIMP int __cdecl _futime(int, struct _utimbuf *);

#if !__STDC__
/* Non-ANSI name for compatibility */
_CRTIMP int __cdecl utime(char *, struct utimbuf *);
#endif

#ifdef __cplusplus
}
#endif

#define _INC_UTIME
#endif	/* _INC_UTIME */
