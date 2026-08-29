/***
*memory.h - declarations for buffer (memory) manipulation routines
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This include file contains the function declarations for the
*	buffer (memory) manipulation routines.
*	[System V]
*
****/

#ifndef _INC_MEMORY

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


#ifndef _SIZE_T_DEFINED
typedef unsigned int size_t;
#define _SIZE_T_DEFINED
#endif

/* function prototypes */

_CRTIMP void * __cdecl _memccpy(void *, const void *, int, unsigned int);
_CRTIMP void * __cdecl memchr(const void *, int, size_t);
	int __cdecl memcmp(const void *, const void *, size_t);
	void * __cdecl memcpy(void *, const void *, size_t);
_CRTIMP int __cdecl _memicmp(const void *, const void *, unsigned int);
	void * __cdecl memset(void *, int, size_t);

#if !__STDC__
/* Non-ANSI names for compatibility */
_CRTIMP void * __cdecl memccpy(void *, const void *, int, unsigned int);
_CRTIMP int __cdecl memicmp(const void *, const void *, unsigned int);
#endif	/* __STDC__ */

#ifdef __cplusplus
}
#endif

#define _INC_MEMORY
#endif	/* _INC_MEMORY */
