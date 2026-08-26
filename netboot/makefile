VASM_DIR := ../third_party/vasm

ifeq ($(OS),Windows_NT)
VASM := $(VASM_DIR)/vasmm68k_mot.exe
else
VASM := $(VASM_DIR)/vasmm68k_mot
endif

VASMFLAGS := -quiet -Fbin -pic

Bootstrap.bin: Bootstrap.a $(VASM)
	$(VASM) $(VASMFLAGS) -o $@ $<

BootstrapFloppy/System.rdump: Bootstrap.bin
	rfx cp $< $@//boot/1

BootstrapFloppy.dsk: BootstrapFloppy/System.rdump
	MakeHFS -n 'NetBoot Enabler' -i BootstrapFloppy -s 1440k -d now $@



BootWrapper.bin: BootWrapper.a $(VASM)
	$(VASM) $(VASMFLAGS) -o $@ $<



ChainLoader.bin: ChainLoader.a BootPicker.a $(VASM)
	$(VASM) $(VASMFLAGS) -o $@ $<



ChainDisk.bin: ChainDisk.a $(VASM)
	$(VASM) $(VASMFLAGS) -o $@ $<


# Build vasm from source if the prebuilt binary isn't checked in (unix-like
# hosts only -- the Windows vasmm68k_mot.exe is committed and never rebuilt
# here).
ifneq ($(OS),Windows_NT)
$(VASM):
	$(MAKE) -C $(VASM_DIR) CPU=m68k SYNTAX=mot
endif
