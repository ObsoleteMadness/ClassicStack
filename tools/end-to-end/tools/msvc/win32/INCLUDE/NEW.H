/***
*new.h - declarations and definitions for C++ memory allocation functions
*
*	Copyright (c) 1990-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	Contains the declarations for C++ memory allocation functions.
*
****/

#ifndef __INC_NEW


#ifndef _CRTIMP
#ifdef _DLL
#define _CRTIMP __declspec(dllimport)
#else
#define _CRTIMP
#endif /* ?CRTDLL */
#endif


/* types and structures */

#ifndef _SIZE_T_DEFINED
typedef unsigned int size_t;
#define _SIZE_T_DEFINED
#endif

typedef int (__cdecl * _PNH)( size_t );

/* function prototypes */

_CRTIMP _PNH __cdecl _set_new_handler( _PNH );

#define __INC_NEW
#endif
