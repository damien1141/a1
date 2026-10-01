# Communication Protocols Reference

Implementation patterns for I2C, SPI, UART, CAN, and 1-Wire on bare-metal MCUs. Read alongside `microcontroller-programming.md` for clock/GPIO/DMA setup.

## Protocol Comparison

| Protocol | Wires | Speed | Topology | Best for |
|---|---|---|---|---|
| **UART** | 2 (TX/RX) + GND | up to ~4 Mbps | Point-to-point | Simple async serial; long-distance with RS-485 |
| **I2C** | 2 (SCL/SDA) + GND | 100kHz/400kHz/1MHz | Multi-drop, addressed | Sensors, EEPROMs; many devices on few pins |
| **SPI** | 4 (MOSI/MISO/SCK/CS) + GND | up to 50+ MHz | Master + slaves (CS each) | High-speed; SD cards, displays, flash |
| **CAN** | 2 (CANH/CANL) | up to 1 Mbps | Differential bus | Automotive; noisy environments |
| **1-Wire** | 1 (data) + GND | ~16 kbps | Multi-drop, addressed | Cheap sensors (DS18B20); iButtons |

## UART

Simplest protocol. Use hardware UART peripheral, not bit-banging.

```c
/* STM32F4 USART2 at 115200 baud on PA2 (TX) / PA3 (RX) */
void uart2_init(void) {
    RCC->AHB1ENR |= RCC_AHB1ENR_GPIOAEN;
    RCC->APB1ENR |= RCC_APB1ENR_USART2EN;

    /* PA2 (TX) and PA3 (RX) as AF7 */
    GPIOA->MODER = (GPIOA->MODER & ~((3U << 4) | (3U << 6))) | (2U << 4) | (2U << 6);
    GPIOA->AFR[0] = (GPIOA->AFR[0] & ~((0xFU << 8) | (0xFU << 12))) | (7U << 8) | (7U << 12);
    GPIOA->OSPEEDR |= (3U << 4) | (3U << 6);

    /* Baud rate: APB1=42MHz, 115200 baud → 42e6/(16*115200) = 22.787 → BRR=0x16C */
    USART2->BRR = (22 << 4) | (12 << 0);  /* or use formula */
    USART2->CR1 = USART_CR1_UE | USART_CR1_TE | USART_CR1_RE | USART_CR1_RXNEIE;

    NVIC_SetPriority(USART2_IRQn, 6);
    NVIC_EnableIRQ(USART2_IRQn);
}

/* Interrupt-driven receive */
volatile uint8_t g_rx_byte;
volatile bool g_rx_ready;

void USART2_IRQHandler(void) {
    if (USART2->SR & USART_SR_RXNE) {
        g_rx_byte = USART2->DR;  /* reading clears RXNE */
        g_rx_ready = true;
    }
}
```

For production: use DMA for both TX and RX. Idle-line detection on RX DMA lets you receive variable-length frames efficiently.

## I2C

Two-wire, addressed, multi-drop. Every device has a 7-bit (or 10-bit) address.

```c
/* I2C1 on PB6 (SCL) / PB7 (SDA) at 400kHz */
void i2c1_init(void) {
    RCC->AHB1ENR |= RCC_AHB1ENR_GPIOBEN;
    RCC->APB1ENR |= RCC_APB1ENR_I2C1EN;

    /* PB6/PB7 as AF4, open-drain */
    GPIOB->MODER = (GPIOB->MODER & ~((3U << 12) | (3U << 14))) | (2U << 12) | (2U << 14);
    GPIOB->OTYPER |= GPIO_OTYPER_OT6 | GPIO_OTYPER_OT7;
    GPIOB->AFR[0] = (GPIOB->AFR[0] & ~((0xFU << 24) | (0xFU << 28))) | (4U << 24) | (4U << 28);
    GPIOB->PUPDR |= (1U << 12) | (1U << 14);  /* pull-up (external resistors still needed) */

    /* Reset I2C */
    I2C1->CR1 = I2C_CR1_SWRST;
    I2C1->CR1 = 0;

    /* Timing for 400kHz on 42MHz APB1 */
    I2C1->CR2 = 42;          /* FREQ */
    I2C1->CCR = (1 << 15) | 35;  /* fast mode, duty 16/9, CCR for 400kHz */
    I2C1->TRISE = 11;        /* 1000ns / (1/42MHz) + 1 */

    I2C1->CR1 = I2C_CR1_PE;  /* enable */
}

/* Write with timeout */
bool i2c_write(uint8_t addr, const uint8_t *data, uint16_t len) {
    uint32_t timeout = 10000;

    I2C1->CR1 |= I2C_CR1_START;
    while (!(I2C1->SR1 & I2C_SR1_SB) && --timeout);
    if (!timeout) return false;

    I2C1->DR = (addr << 1);  /* write */
    timeout = 10000;
    while (!(I2C1->SR1 & I2C_SR1_ADDR) && --timeout);
    if (!timeout) return false;
    (void)I2C1->SR1; (void)I2C1->SR2;  /* clear ADDR */

    for (uint16_t i = 0; i < len; i++) {
        timeout = 10000;
        while (!(I2C1->SR1 & I2C_SR1_TXE) && --timeout);
        if (!timeout) return false;
        I2C1->DR = data[i];
    }

    timeout = 10000;
    while (!(I2C1->SR1 & I2C_SR1_BTF) && --timeout);
    I2C1->CR1 |= I2C_CR1_STOP;
    return timeout != 0;
}
```

### I2C Rules

- **Pull-up resistors required** (typically 4.7kΩ for 100kHz, 2.2kΩ for 400kHz)
- **Address is 7-bit** — `addr << 1` for the wire (with R/W bit appended)
- **Clock stretching** — slow slaves can hold SCL low; master must handle this
- **Multi-master** — possible but complex (arbitration); most designs are single-master
- **Bus capacitance limit** — 400pF max; long buses need buffer chips
- **Watch for stuck bus** — if a reset happens mid-transfer, SDA stays low. Clock SCL 9 times to release.

## SPI

Full-duplex, fast, but needs a CS line per slave.

```c
/* SPI1 on PA5 (SCK) / PA6 (MISO) / PA7 (MOSI), CS on PA4 */
void spi1_init(void) {
    RCC->AHB1ENR |= RCC_AHB1ENR_GPIOAEN;
    RCC->APB2ENR |= RCC_APB2ENR_SPI1EN;

    /* SCK/MISO/MOSI as AF5, CS as output */
    GPIOA->MODER = (GPIOA->MODER & ~((3U << 8) | (3U << 10) | (3U << 12) | (3U << 14) | (3U << 16)))
                 | (2U << 10) | (2U << 12) | (2U << 14)  /* AF5 for SCK/MISO/MOSI */
                 | (1U << 8);                              /* output for CS */
    GPIOA->AFR[0] = (GPIOA->AFR[0] & ~((0xFU << 20) | (0xFU << 24) | (0xFU << 28)))
                  | (5U << 20) | (5U << 24) | (5U << 28);
    GPIOA->OSPEEDR |= (3U << 10) | (3U << 12) | (3U << 14);  /* very high speed */

    /* CS high (idle) */
    GPIOA->BSRR = (1U << 4) << 16;  /* reset PA4 */

    /* SPI config: master, baud rate /8 (84MHz/8=10.5MHz), CPOL=0 CPHA=0 */
    SPI1->CR1 = SPI_CR1_MSTR | SPI_CR1_BR_1 | SPI_CR1_SSM | SPI_CR1_SSI;
    SPI1->CR1 |= SPI_CR1_SPE;
}

uint8_t spi_transfer(uint8_t tx) {
    while (!(SPI1->SR & SPI_SR_TXE));
    SPI1->DR = tx;
    while (!(SPI1->SR & SPI_SR_RXNE));
    return SPI1->DR;
}

void spi_read_register(uint8_t reg, uint8_t *buf, uint16_t len) {
    GPIOA->BSRR = (1U << 4) << 16;  /* CS low */
    spi_transfer(reg | 0x80);       /* read bit (sensor-specific) */
    for (uint16_t i = 0; i < len; i++) {
        buf[i] = spi_transfer(0xFF);  /* dummy byte to clock data in */
    }
    GPIOA->BSRR = (1U << 4);        /* CS high */
}
```

### SPI Rules

- **CPOL/CPHA** — 4 modes; check device datasheet. Most sensors use Mode 0 (CPOL=0, CPHA=0).
- **CS per slave** — only one slave selected at a time
- **MSB first** by default (LSB-first is rare; check datasheet)
- **DMA recommended** for >64-byte transfers
- **Half-duplex / 3-wire variants** exist; check your peripheral

## CAN

Differential bus, robust against noise, used in automotive and industrial.

```c
/* STM32F4 CAN1 on PA11 (RX) / PA12 (TX), 500 kbps */
void can1_init(void) {
    RCC->AHB1ENR |= RCC_AHB1ENR_GPIOAEN;
    RCC->APB1ENR |= RCC_APB1ENR_CAN1EN;

    GPIOA->MODER = (GPIOA->MODER & ~((3U << 22) | (3U << 24))) | (2U << 22) | (2U << 24);
    GPIOA->AFR[1] = (GPIOA->AFR[1] & ~((0xFU << 12) | (0xFU << 16))) | (9U << 12) | (9U << 16);

    CAN1->MCR = CAN_MCR_INRQ;       /* enter init mode */
    while (!(CAN1->MSR & CAN_MSR_INAK));

    /* Baud rate: APB1=42MHz, 500kbps, sample point 87.5% */
    /* Bit time = SYNC(1) + BS1(13) + BS2(2) = 16 TQ */
    /* Prescaler = 42MHz / (16 * 500kHz) = 5.25 → not exact; use prescaler=6, BS1=11, BS2=2 */
    CAN1->BTR = (5 << 16) | (11 << 16) | (2 << 20);  /* prescaler, BS1, BS2 */

    CAN1->MCR &= ~CAN_MCR_INRQ;     /* leave init mode */
    while (CAN1->MSR & CAN_MSR_INAK);

    /* Filter: accept all */
    CAN1->FMR |= CAN_FMR_FINIT;
    CAN1->sFilterRegister[0].FR1 = 0;
    CAN1->sFilterRegister[0].FR2 = 0;
    CAN1->FA1R = 1;
    CAN1->FMR &= ~CAN_FMR_FINIT;

    /* Enable receive interrupt */
    CAN1->IER |= CAN_IER_FMPIE0;
    NVIC_SetPriority(CAN1_RX0_IRQn, 5);
    NVIC_EnableIRQ(CAN1_RX0_IRQn);
}

void CAN1_RX0_IRQHandler(void) {
    CanRxMsgTypeDef msg;
    CAN_Receive(CAN1, CAN_FIFO0, &msg);
    /* process msg */
}
```

### CAN Rules

- **Termination resistors** (120Ω) at each end of the bus
- **Standard (11-bit) or Extended (29-bit) IDs**
- **Arbitration** — lowest ID wins; non-destructive
- **Error frames** — bus errors are detected and signaled
- **Bit timing is critical** — use a calculator (http://www.bittiming.can-wiki.info/)

## 1-Wire

Single data wire with parasitic power. Slow but useful for cheap sensors.

```c
/* Bit-banged 1-Wire on PA0 (with 4.7kΩ pull-up to VCC) */
#define OW_PIN  0
#define OW_PORT GPIOA
#define OW_LOW()  (OW_PORT->BSRR = (1U << OW_PIN) << 16)
#define OW_HIGH() (OW_PORT->BSRR = (1U << OW_PIN))
#define OW_READ() ((OW_PORT->IDR >> OW_PIN) & 1)

bool ow_reset(void) {
    bool presence;
    OW_LOW();
    delay_us(480);      /* 480µs reset pulse */
    OW_HIGH();
    delay_us(70);
    presence = (OW_READ() == 0);  /* device pulls low */
    delay_us(410);
    return presence;
}

void ow_write_bit(bool bit) {
    OW_LOW();
    if (bit) delay_us(6); else delay_us(60);
    OW_HIGH();
    delay_us(64);
}

bool ow_read_bit(void) {
    bool bit;
    OW_LOW();
    delay_us(6);
    OW_HIGH();
    delay_us(9);
    bit = OW_READ();
    delay_us(55);
    return bit;
}
```

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Forgot I2C pull-ups | No ACK | Add 4.7kΩ external pull-ups |
| Wrong CPOL/CPHA | SPI garbage | Check datasheet; scope the clock |
| CAN no termination | Intermittent errors | 120Ω at each end |
| Baud rate mismatch | Garbage data | Verify APB clock + baud register calc |
| CS not toggled | Wrong slave | Toggle CS per transaction |
| Long ISR for UART | Bytes lost | Use DMA |
| Wrong I2C address | No ACK | Check 7-bit vs 8-bit (with R/W) |
| Bit-banging too slow | Protocol errors | Use hardware peripheral |
