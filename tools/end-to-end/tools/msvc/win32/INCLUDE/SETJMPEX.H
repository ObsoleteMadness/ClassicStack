/***
*setjmp.h - definitions/declarations for extended setjmp/longjmp routines
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	"extended" (SEH-safe) setjmp.h.  Currently does nothing but include
*	the real setjmp.h, which is already SEH-safe.
*
****/

#ifndef _INC_SETJMP
#include <setjmp.h>
#endif
