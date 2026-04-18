package afp

import (
	"log"
	"time"
)

func (s *AFPService) handleGetSrvrInfo(req *FPGetSrvrInfoReq) (*FPGetSrvrInfoRes, error) {
	return &FPGetSrvrInfoRes{
		MachineType: "Macintosh",
		AFPVersions: []string{Version20, Version21},
		UAMs:        []string{UAMNoUserAuthent},
		ServerName:  s.ServerName,
		Flags:       0x0001 | 0x0002,
	}, nil
}

func (s *AFPService) handleGetSrvrParms(req *FPGetSrvrParmsReq) (*FPGetSrvrParmsRes, int32) {
	s.mu.RLock()
	defer s.mu.RUnlock()

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

func (s *AFPService) handleLogin(req *FPLoginReq) (*FPLoginRes, int32) {
	log.Printf("[AFP] Login attempt: Version=%q, UAM=%q", req.AFPVersion, req.UAM)

	if req.AFPVersion != Version20 && req.AFPVersion != Version21 {
		return &FPLoginRes{}, ErrBadVersNum
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if req.UAM == UAMNoUserAuthent {
		// Nothing else required
	} else if req.UAM == UAMCleartxtPasswd {
		log.Printf("[AFP] Cleartxt Passwrd for User=%q", req.Username)
		expectedPw, exists := s.users[req.Username]
		if !exists || expectedPw != req.Password {
			return &FPLoginRes{}, ErrUserNotAuth
		}
	} else {
		return &FPLoginRes{}, ErrBadUAM
	}

	sRefNum := s.nextSRefNum
	s.nextSRefNum++

	return &FPLoginRes{
		SRefNum:  sRefNum,
		IDNumber: 0,
	}, NoErr
}

// AddUser adds a user to the AFP service for authentication.
func (s *AFPService) AddUser(username, password string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[username] = password
}

func (s *AFPService) handleLogout(req *FPLogoutReq) (*FPLogoutRes, int32) {
	return &FPLogoutRes{}, NoErr
}

func (s *AFPService) handleMapID(req *FPMapIDReq) (*FPMapIDRes, int32) {
	name := "root"
	if req.Function == 2 || req.Function == 4 {
		name = "wheel"
	}
	return &FPMapIDRes{Name: name}, NoErr
}

func (s *AFPService) handleMapName(req *FPMapNameReq) (*FPMapNameRes, int32) {
	return &FPMapNameRes{ID: 0}, NoErr
}
