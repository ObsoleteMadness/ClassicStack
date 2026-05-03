//go:build afp || all

package afp

import (
	"time"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
)

func (s *Service) handleGetSrvrInfo(req *FPGetSrvrInfoReq) (*FPGetSrvrInfoRes, error) {
	return &FPGetSrvrInfoRes{
		MachineType: "Macintosh",
		AFPVersions: []string{Version20, Version21},
		UAMs:        []string{UAMNoUserAuthent},
		ServerName:  s.ServerName,
		Flags:       0x0001 | 0x0002,
	}, nil
}

func (s *Service) handleGetSrvrParms(req *FPGetSrvrParmsReq) (*FPGetSrvrParmsRes, int32) {
	res := &FPGetSrvrParmsRes{
		ServerTime: toAFPTime(time.Now()),
		Volumes:    make([]VolInfo, len(s.Volumes)),
	}

	for i, vol := range s.Volumes {
		flags := uint8(0)
		if vol.Config.Password != "" {
			flags |= VolInfoFlagHasPassword
		}
		res.Volumes[i] = VolInfo{
			Flags: flags,
			Name:  vol.Config.Name,
		}
	}

	return res, NoErr
}

func (s *Service) handleLogin(req *FPLoginReq) (*FPLoginRes, int32) {
	netlog.Debug("[AFP] Login attempt: Version=%q, UAM=%q", req.AFPVersion, req.UAM)

	if req.AFPVersion != Version20 && req.AFPVersion != Version21 {
		return &FPLoginRes{}, ErrBadVersNum
	}

	if req.UAM == UAMNoUserAuthent {
		// Nothing else required
	} else if req.UAM == UAMCleartxtPasswd {
		netlog.Debug("[AFP] Cleartxt Passwrd for User=%q", req.Username)
		if !s.sessions.checkPassword(req.Username, req.Password) {
			return &FPLoginRes{}, ErrUserNotAuth
		}
	} else {
		return &FPLoginRes{}, ErrBadUAM
	}

	return &FPLoginRes{
		SRefNum:  s.sessions.allocSRef(),
		IDNumber: 0,
	}, NoErr
}

// AddUser adds a user to the AFP service for authentication.
func (s *Service) AddUser(username, password string) {
	s.sessions.addUser(username, password)
}

func (s *Service) handleLogout(req *FPLogoutReq) (*FPLogoutRes, int32) {
	return &FPLogoutRes{}, NoErr
}

func (s *Service) handleMapID(req *FPMapIDReq) (*FPMapIDRes, int32) {
	name := "root"
	if req.Function == 2 || req.Function == 4 {
		name = "wheel"
	}
	return &FPMapIDRes{Name: name}, NoErr
}

func (s *Service) handleMapName(req *FPMapNameReq) (*FPMapNameRes, int32) {
	return &FPMapNameRes{ID: 0}, NoErr
}
