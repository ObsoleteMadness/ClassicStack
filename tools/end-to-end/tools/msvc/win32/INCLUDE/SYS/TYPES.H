/***
*sys/types.h - types returned by system level calls for file and time info
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This file defines types used in defining values returned by system
*	level calls for file status and time information.
*	[System V]
*
****/

#ifndef _INC_TYPES

#ifndef _TIME_T_DEFINED
typedef long time_t;
#define _TIME_T_DEFINED
#endif

#ifndef _INO_T_DEFINED
typedef unsigned short _ino_t;		/* i-node number (not used on DOS) */
#if !__STDC__
/* Non-ANSI name for compatibility */
typedef unsigned short ino_t;		/* i-node number (not used on DOS) */
#endif
#define _INO_T_DEFINED
#endif

#ifndef _DEV_T_DEFINED
typedef unsigned int _dev_t;			/* device code */
#if !__STDC__
/* Non-ANSI name for compatibility */
typedef unsigned int dev_t;			/* device code */
#endif
#define _DEV_T_DEFINED
#endif

#ifndef _OFF_T_DEFINED
typedef long _off_t;			/* file offset value */
#if !__STDC__
/* Non-ANSI name for compatibility */
typedef long off_t;			/* file offset value */
#endif
#define _OFF_T_DEFINED
#endif

#define _INC_TYPES
#endif	/* _INC_TYPES */
