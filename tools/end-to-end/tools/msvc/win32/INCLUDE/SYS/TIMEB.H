/***
*sys/timeb.h - definition/declarations for _ftime()
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This file define the _ftime() function and the types it uses.
*	[System V]
*
****/

#ifndef _INC_TIMEB

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

/* structure returned by _ftime system call */

#ifndef _TIMEB_DEFINED
struct _timeb {
	time_t time;
	unsigned short millitm;
	short timezone;
	short dstflag;
	};

#if !__STDC__
struct timeb {
	time_t time;
	unsigned short millitm;
	short timezone;
	short dstflag;
	};
#endif

#define _TIMEB_DEFINED
#endif

/* function prototypes */

_CRTIMP void __cdecl _ftime(struct _timeb *);

#if !__STDC__
/* Non-ANSI name for compatibility */
_CRTIMP void __cdecl ftime(struct timeb *);
#endif

#ifdef __cplusplus
}
#endif

#define _INC_TIMEB
#endif	/* _INC_TIMEB */
