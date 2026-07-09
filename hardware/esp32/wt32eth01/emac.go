//go:build esp32 && wt32eth01

package wt32eth01

/*
#cgo LDFLAGS: -lesp_eth
#include <esp_eth.h>
#include <esp_event.h>
#include <driver/gpio.h>

void go_eth_recv_cb(uint32_t length, uint8_t *buffer);

static esp_err_t eth_recv_cb(esp_eth_handle_t eth_handle, uint8_t *buffer, uint32_t length, void *priv) {
    go_eth_recv_cb(length, buffer);
    free(buffer); // ESP-IDF requires the receiver to free the buffer
    return ESP_OK;
}

static esp_eth_handle_t init_emac_driver(int mdc, int mdio, int power, int phy_addr) {
    // 1. Setup MAC configuration
    eth_mac_config_t mac_config = ETH_MAC_DEFAULT_CONFIG();
    mac_config.smi_mdc_gpio_num = mdc;
    mac_config.smi_mdio_gpio_num = mdio;

    // 2. Setup PHY configuration
    eth_phy_config_t phy_config = ETH_PHY_DEFAULT_CONFIG();
    phy_config.phy_addr = phy_addr;
    phy_config.reset_gpio_num = power;

    // 3. Create MAC and PHY instances
    esp_eth_mac_t *mac = esp_eth_mac_new_esp32(&mac_config);
    esp_eth_phy_t *phy = esp_eth_phy_new_lan87xx(&phy_config);

    // 4. Install Ethernet driver
    esp_eth_config_t config = ETH_DEFAULT_CONFIG(mac, phy);
    config.stack_input = eth_recv_cb;

    esp_eth_handle_t eth_handle = NULL;
    if (esp_eth_driver_install(&config, &eth_handle) == ESP_OK) {
        return eth_handle;
    }
    return NULL;
}
*/
import "C"
import (
	"errors"
	"unsafe"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

type emacLink struct {
	handle C.esp_eth_handle_t
	recvCh chan []byte
	closed bool
}

var activeEmac *emacLink

//export go_eth_recv_cb
func go_eth_recv_cb(length C.uint32_t, buffer *C.uint8_t) {
	if activeEmac == nil || activeEmac.closed {
		return
	}
	data := C.GoBytes(unsafe.Pointer(buffer), C.int(length))
	select {
	case activeEmac.recvCh <- data:
	default:
		// Queue full, drop packet
	}
}

// Compile-time assertion: *emacLink satisfies link.FrameLink.
var _ link.FrameLink = (*emacLink)(nil)

func OpenEMAC(mdc, mdio, power int, phyAddr int) (link.FrameLink, error) {
	handle := C.init_emac_driver(C.int(mdc), C.int(mdio), C.int(power), C.int(phyAddr))
	if handle == nil {
		return nil, errors.New("emac: failed to initialize ESP32 Ethernet driver")
	}

	el := &emacLink{
		handle: handle,
		recvCh: make(chan []byte, 64),
	}
	activeEmac = el

	// Start the Ethernet driver
	if C.esp_eth_start(handle) != C.ESP_OK {
		C.esp_eth_driver_uninstall(handle)
		return nil, errors.New("emac: failed to start Ethernet driver")
	}

	return el, nil
}

func (l *emacLink) Read() (link.Frame, error) {
	if l.closed {
		return nil, link.ErrClosed
	}
	frame, ok := <-l.recvCh
	if !ok {
		return nil, link.ErrClosed
	}
	return frame, nil
}

func (l *emacLink) Write(frame link.Frame) error {
	if l.closed {
		return link.ErrClosed
	}
	ptr := unsafe.Pointer(&frame[0])
	res := C.esp_eth_transmit(l.handle, ptr, C.uint32_t(len(frame)))
	if res != C.ESP_OK {
		return errors.New("emac: transmission failed")
	}
	return nil
}

func (l *emacLink) Close() error {
	if l.closed {
		return nil
	}
	l.closed = true
	close(l.recvCh)
	C.esp_eth_stop(l.handle)
	C.esp_eth_driver_uninstall(l.handle)
	activeEmac = nil
	return nil
}
