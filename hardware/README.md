# ClassicStack Hardware Wiring Guide

This document describes how to connect TashTalk, SD-Card readers, and the LAN8720A Ethernet PHY to the supported microcontroller boards (**WT32-ETH01** and **Raspberry Pi Pico / Pico W / Pico 2 / Pico 2 W**).

---

## 1. WT32-ETH01 (ESP32)

The WT32-ETH01 has an onboard LAN8720A PHY. We connect TashTalk and an SPI SD-Card reader to the remaining GPIO pins.

### WT32-ETH01 ASCII Pinout Diagram

```text
                     +-------------------+
             EN [ ]  |                   |  [ ] TXD (IO1) - TXD0
            GND [ ]  |     WT32-S1       |  [ ] RXD (IO3) - RXD0
            3V3 [ ]  |  (ESP32-D0WDQ6)   |  [ ] IO0
             EN [ ]  |                   |  [ ] GND
     CFG (IO32) [ ]  |                   |  [ ] IO39 (Input Only)
  485_EN (IO33) [ ]  |  +-------------+  |  [ ] IO36 (Input Only)
    RXD2 (IO5)  [ ]  |  |   WiFi      |  |  [ ] IO15 -------> SD MISO
    TXD2 (IO17) [ ]  |  |   Antenna   |  |  [ ] IO14 -------> SD CLK
            GND [ ]  |  +-------------+  |  [ ] IO12 -------> SD MOSI
            3V3 [ ]  |                   |  [ ] IO35 <------- TashTalk CTS
            GND [ ]  |                   |  [ ] IO4  -------> SD CS
             5V [ ]  |                   |  [ ] IO2
           LINK [ ]  |     [RJ-45]       |  [ ] GND
                     +-------------------+
```

### WT32-ETH01 Peripheral Connections

#### TashTalk (Secondary UART2)
Connect TashTalk to the secondary UART. Note that `IO35` is an input-only pin, which is perfect for receiving the active-low `CTS` signal from the TashTalk.

| TashTalk Pin | WT32-ETH01 Pin | Type | Notes |
| :--- | :--- | :--- | :--- |
| **VCC** | **3V3** or **5V** | Power | Match TashTalk supply voltage |
| **GND** | **GND** | Ground | Common ground |
| **RX** | **TXD2 (IO17)** | Output | Serial transmit from ESP32 |
| **TX** | **RXD2 (IO5)** | Input | Serial receive to ESP32 |
| **CTS** | **IO35** | Input | Hardware flow control (Input Only) |

#### SD-Card Reader (SPI)
Connect the SD-Card reader to the hardware SPI interface:

| SD Card Pin | WT32-ETH01 Pin | Type | Notes |
| :--- | :--- | :--- | :--- |
| **VCC** | **3V3** | Power | SD Card supply |
| **GND** | **GND** | Ground | Common ground |
| **MISO** | **IO15** | Input | Master In Slave Out |
| **CLK** | **IO14** | Output | Serial Clock |
| **MOSI** | **IO12** | Output | Master Out Slave In |
| **CS** | **IO4** | Output | Chip Select |

---

## 2. Raspberry Pi Pico W / Pico 2 W

For the Pico W family, we connect the external **LAN8720A PHY** using a PIO-based RMII state machine, along with the TashTalk and SD-Card reader.

### Pico W / Pico 2 W ASCII Pinout Diagram

```text
                        +--------------------+
    TashTalk RX <- GP0 [ ] 1             40 [ ] VBUS
  TashTalk TX -> GP1 [ ] 2             39 [ ] VSYS
                 GND [ ] 3             38 [ ] GND
 TashTalk CTS -> GP2 [ ] 4             37 [ ] 3V3_EN
                 GP3 [ ] 5             36 [ ] 3V3(OUT) -----> VCC (3.3V)
       SD MISO <- GP4 [ ] 6             35 [ ] ADC_VREF
         SD CS <- GP5 [ ] 7             34 [ ] GP28 (ADC2)
                 GND [ ] 8             33 [ ] GND
        SD CLK <- GP6 [ ] 9             32 [ ] GP27 --------> RMII TXD1
       SD MOSI <- GP7 [ ] 10            31 [ ] GP26 --------> RMII REFCLK
                 GP8 [ ] 11            30 [ ] RUN
                 GP9 [ ] 12            29 [ ] GP22 --------> RMII TX_EN
                 GND [ ] 13            28 [ ] GND
                GP10 [ ] 14            27 [ ] GP21 --------> RMII RXD1
                GP11 [ ] 15            26 [ ] GP20 --------> RMII RXD0
                GP12 [ ] 16            25 [ ] GP19 --------> RMII TXD0
                GP13 [ ] 17            24 [ ] GP18 --------> RMII CRS_DV
                 GND [ ] 18            23 [ ] GND
      RMII MDC <- GP14 [ ] 19            22 [ ] GP17
     RMII MDIO <- GP15 [ ] 20            21 [ ] GP16
                     +-------[USB]-------+
```

### Pico W / Pico 2 W Peripheral Connections

#### TashTalk (UART0)
Connect TashTalk to UART0.

| TashTalk Pin | Pico Pin | GPIO Pin | Notes |
| :--- | :--- | :--- | :--- |
| **VCC** | **Pin 36** | **3V3(OUT)** | Power supply |
| **GND** | **Pin 3 / 8 / 13 / 23 / 28 / 38** | **GND** | Common ground |
| **RX** | **Pin 1** | **GP0** | Serial transmit from Pico |
| **TX** | **Pin 2** | **GP1** | Serial receive to Pico |
| **CTS** | **Pin 4** | **GP2** | Hardware flow control (Input to Pico) |

#### SD-Card Reader (SPI0)
Connect the SD-Card reader to SPI0.

| SD Card Pin | Pico Pin | GPIO Pin | Type | Notes |
| :--- | :--- | :--- | :--- | :--- |
| **VCC** | **Pin 36** | **3V3(OUT)** | Power supply |
| **GND** | **Pin 3 / 8 / 13 / 23 / 28 / 38** | **GND** | Common ground |
| **MISO** | **Pin 6** | **GP4** | Master In Slave Out |
| **CS** | **Pin 7** | **GP5** | Chip Select |
| **CLK** | **Pin 9** | **GP6** | Serial Clock |
| **MOSI** | **Pin 10** | **GP7** | Master Out Slave In |

#### Waveshare LAN8720A Ethernet Board (RMII - PIO-based)
Connect the Waveshare LAN8720A module via the PIO-based RMII layout.

| Waveshare Pin | Pico Pin | GPIO Pin | Type | Notes |
| :--- | :--- | :--- | :--- | :--- |
| **VCC** | **Pin 36** | **3V3(OUT)** | Power | 3.3V power |
| **GND** | **Pin 3 / 8 / 13 / 23 / 28 / 38** | **GND** | Ground | Common ground |
| **MDC** | **Pin 19** | **GP14** | Output | Management clock |
| **MDIO** | **Pin 20** | **GP15** | Bidirectional | Management data |
| **RXD0** | **Pin 26** | **GP20** | Input | RMII receive data 0 |
| **RXD1** | **Pin 27** | **GP21** | Input | RMII receive data 1 |
| **CRS_DV** | **Pin 24** | **GP18** | Input | Carrier Sense / Data Valid |
| **TXD0** | **Pin 25** | **GP19** | Output | RMII transmit data 0 |
| **TXD1** | **Pin 32** | **GP27** | Output | RMII transmit data 1 |
| **TX_EN** | **Pin 29** | **GP22** | Output | Transmit enable |
| **REFCLK** | **Pin 31** | **GP26** | Input | 50MHz reference clock |
| **nRST** | **Pin 36** | **3V3(OUT)** | Input | Reset (Active Low), connect to 3V3 |

#### W5500 Ethernet Board (SPI-based)
Connect the W5500 SPI-to-Ethernet module to SPI1.

| W5500 Pin | Pico Pin | GPIO Pin | Type | Notes |
| :--- | :--- | :--- | :--- | :--- |
| **VCC** | **Pin 36** | **3V3(OUT)** | Power | 3.3V power |
| **GND** | **Pin 3 / 8 / 13 / 23 / 28 / 38** | **GND** | Ground | Common ground |
| **SCLK** | **Pin 14** | **GP10** | Output | SPI1 Clock |
| **MOSI** | **Pin 15** | **GP11** | Output | SPI1 Master Out |
| **MISO** | **Pin 16** | **GP12** | Input | SPI1 Master In |
| **SCS** | **Pin 17** | **GP13** | Output | Chip Select (Active Low) |
| **RST** | **Pin 11** | **GP8** | Output | Reset (Active Low) |
| **INT** | **Pin 12** | **GP9** | Input | Interrupt |
