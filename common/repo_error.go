package common

import (
	"errors"

	"oss/adaptor/repo/repoerr"
)

func ErrnoFromRepoError(err error, defaultErr Errno) Errno {
	return ErrnoFromRepoErrorWithNotFound(err, defaultErr, ResouceNotFoundErr)
}

func ErrnoFromRepoErrorWithNotFound(err error, defaultErr, notFoundErr Errno) Errno {
	if err == nil {
		return defaultErr
	}

	switch {
	case errors.Is(err, repoerr.ErrNotFound):
		return notFoundErr.WithErr(err)
	case errors.Is(err, repoerr.ErrDuplicate):
		return ConflictErr.WithErr(err)
	case errors.Is(err, repoerr.ErrFKViolated):
		return DatabaseErr.WithMsg("Foreign key violated").WithErr(err)
	case errors.Is(err, repoerr.ErrInvalidData):
		return ParamErr.WithErr(err)
	default:
		return defaultErr.WithErr(err)
	}
}
