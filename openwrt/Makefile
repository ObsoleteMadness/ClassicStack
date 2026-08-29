# OpenWRT package Makefile for ClassicStack.
#
# Drop this directory into an OpenWRT buildroot or feed as
# package/network/services/classicstack, then:
#
#   ./scripts/feeds update -a && ./scripts/feeds install classicstack
#   make menuconfig            # Network -> classicstack
#   make package/classicstack/compile V=s
#
# It uses the buildroot's golang-package infrastructure to cross-compile
# ./cmd/classicstack with the file-service + legacy-transport build tags and
# installs the procd init script and the UCI default config.

include $(TOPDIR)/rules.mk

PKG_NAME:=classicstack
PKG_VERSION:=0.1.0
PKG_RELEASE:=1

# Point PKG_SOURCE at a release tarball of this repo, or use a git checkout. The
# placeholder below assumes the source is fetched as a tarball; adjust the URL/hash
# for a real release, or switch to PKG_SOURCE_PROTO:=git with PKG_SOURCE_URL/VERSION.
PKG_SOURCE_PROTO:=git
PKG_SOURCE_URL:=https://github.com/ObsoleteMadness/ClassicStack.git
PKG_SOURCE_VERSION:=HEAD
PKG_SOURCE:=$(PKG_NAME)-$(PKG_VERSION).tar.gz
PKG_MIRROR_HASH:=skip

PKG_LICENSE:=MIT
PKG_LICENSE_FILES:=LICENSE
PKG_MAINTAINER:=ClassicStack contributors

# The Go import path of the module (go.mod module path).
GO_PKG:=github.com/ObsoleteMadness/ClassicStack
# Build tags: the file services + legacy transports + libpcap. Trim this to suit a
# smaller image (e.g. "afp pcap" for an AFP-only router).
GO_PKG_BUILD_TAGS:=afp smb netbios ipx netbeui macip pcap
# Link-time build metadata.
GO_PKG_LDFLAGS_X:=main.BuildVersion=$(PKG_VERSION) main.BuildCommit=$(PKG_SOURCE_VERSION) main.BuildDate=openwrt

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/classicstack
  SECTION:=net
  CATEGORY:=Network
  SUBMENU:=Services
  TITLE:=ClassicStack AppleTalk / AFP / SMB legacy file server
  URL:=https://github.com/ObsoleteMadness/ClassicStack
  DEPENDS:=$(GO_ARCH_DEPENDS) +libpcap
endef

define Package/classicstack/description
  ClassicStack is a Go AppleTalk Phase 2 router and AFP file server that also
  speaks NetBEUI / NetBIOS / SMB and bridges legacy Apple/Novell networking to
  modern transports (EtherTalk, LToUDP, TashTalk, IPX). This package builds the
  classicstack daemon and installs a procd service plus a UCI config at
  /etc/config/classicstack.
endef

# The compiled command lives at ./cmd/classicstack within the module.
GO_PKG_BUILD_PKG:=$(GO_PKG)/cmd/classicstack

define Package/classicstack/conffiles
/etc/config/classicstack
endef

define Package/classicstack/install
	$(call GoPackage/Package/Install/Bin,$(1))
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/classicstack.init $(1)/etc/init.d/classicstack
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/classicstack.config $(1)/etc/config/classicstack
endef

$(eval $(call GoPackage/classicstack))
$(eval $(call BuildPackage,classicstack))
