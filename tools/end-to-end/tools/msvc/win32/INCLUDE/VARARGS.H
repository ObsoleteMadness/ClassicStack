/***
*varargs.h - XENIX style macros for variable argument functions
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This file defines XENIX style macros for accessing arguments of a
*	function which takes a variable number of arguments.
*	[System V]
*
****/

#ifndef _INC_VARARGS

#ifdef __cplusplus
extern "C" {
#endif

#if	__STDC__
#error varargs.h incompatible with ANSI (use stdarg.h)
#endif

#ifndef _VA_LIST_DEFINED
typedef char *va_list;
#define _VA_LIST_DEFINED
#endif

#ifdef	_M_IX86
/*
 * define a macro to compute the size of a type, variable or expression,
 * rounded up to the nearest multiple of sizeof(int). This number is its
 * size as function argument (Intel architecture). Note that the macro
 * depends on sizeof(int) being a power of 2!
 */
#define _INTSIZEOF(n)	 ( (sizeof(n) + sizeof(int) - 1) & ~(sizeof(int) - 1) )

#define va_dcl va_list va_alist;
#define va_start(ap) ap = (va_list)&va_alist
#define va_arg(ap,t)    ( *(t *)((ap += _INTSIZEOF(t)) - _INTSIZEOF(t)) )
#define va_end(ap) ap = (va_list)0
#endif

#ifdef __cplusplus
}
#endif

#define _INC_VARARGS
#endif	/* _INC_VARARGS */
