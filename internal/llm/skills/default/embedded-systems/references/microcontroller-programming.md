# Microcontroller Programming Reference

Bare-metal C/C++ on ARM Cortex-M and similar MCUs. Covers clocks, peripherals, GPIO/ADC/timers, interrupts, DMA, and the HAL layer. Read alongside `rtos-patterns.md` for task-based designs.

## Clock Tree (STM32F4 example)

Every peripheral needs its clock enabled before use. Forgetting this is the #1 bare-metal bug.

```c
/* Enable clocks BEFORE touching any register */
RCC->AHB1ENR |= RCC_AHB1ENR_GPIOAEN;    /* GPIO port A */
RCC->AHB1ENR |= RCC_AHB1ENR_DMA2EN;     /* DMA2 */
RCC->APB1ENR |= RCC_APB1ENR_USART2EN;   /* USART2 */
RCC->APB1ENR |= RCC_APB1ENR_TIM2EN;     /* TIM2 */
RCC->APB2ENR |= RCC_APB2ENR_ADC1EN;     /* ADC1 */
```

Always check the reference manual for which bus (AHB1/APB1/APB2) each peripheral is on.

## GPIO Configuration

```c
/* GPIO modes: input, output, alternate function, analog */
typedef enum {
    GPIO_MODE_INPUT   = 0x00,
    GPIO_MODE_OUTPUT  = 0x01,
    GPIO_MODE_AF      = 0x02,
    GPIO_MODE_ANALOG  = 0x03,
} GPIO_Mode_t;

void gpio_config_output(GPIO_TypeDef *port, uint8_t pin) {
    /* Enable clock */
    if (port == GPIOA)      RCC->AHB1ENR |= RCC_AHB1ENR_GPIOAEN;
    else if (port == GPIOB) RCC->AHB1ENR |= RCC_AHB1ENR_GPIOBEN;
    /* ... etc */

    /* Set mode (2 bits per pin) */
    port->MODER &= ~(3U << (pin * 2));
    port->MODER |= (GPIO_MODE_OUTPUT << (pin * 2));

    /* Set output type: push-pull (default) or open-drain */
    port->OTYPER &= ~(1U << pin);  /* 0 = push-pull */

    /* Set speed: low/medium/high/very-high */
    port->OSPEEDR &= ~(3U << (pin * 2));
    port->OSPEEDR |= (2U << (pin * 2));  /* high speed */

    /* Set pull-up/pull-down: none/up/down */
    port->PUPDR &= ~(3U << (pin * 2));
    /* (no pull for output) */
}

void gpio_write(GPIO_TypeDef *port, uint8_t pin, bool high) {
    if (high) {
        port->BSRR = (1U << pin);          /* atomic set */
    } else {
        port->BSRR = (1U << (pin + 16));   /* atomic reset */
    }
}
```

**Atomic access:** Use `BSRR` (bit set/reset) — never `ODR |=` (read-modify-write, race-prone with ISRs).

## ADC: Single Conversion

```c
void adc1_init(void) {
    RCC->APB2ENR |= RCC_APB2ENR_ADC1EN;

    /* Configure PA0 as analog */
    RCC->AHB1ENR |= RCC_AHB1ENR_GPIOAEN;
    GPIOA->MODER |= (3U << (0 * 2));   /* analog mode */
    GPIOA->PUPDR &= ~(3U << (0 * 2));  /* no pull */

    /* ADC common settings */
    ADC->CCR = 0;                       /* independent mode, no prescaler */
    ADC1->CR1 = 0;
    ADC1->CR2 = ADC_CR2_ADON;           /* enable ADC */
    ADC1->SQR3 = 0;                     /* channel 0 first in sequence */
    ADC1->SMPR1 = (7U << 0);            /* 480 cycles sample time for ch0 */

    /* Wait for ADC to stabilize */
    for (volatile int i = 0; i < 10000; i++);
}

uint16_t adc1_read(uint8_t channel) {
    ADC1->SQR3 = channel;               /* select channel */
    ADC1->CR2 |= ADC_CR2_SWSTART;       /* start conversion */
    while (!(ADC1->SR & ADC_SR_EOC));   /* wait for completion */
    return (uint16_t)ADC1->DR;          /* reading DR clears EOC */
}
```

For production: use DMA for continuous sampling (see below); never poll the ADC in a tight loop.

## Timers: PWM and Periodic Interrupts

```c
/* TIM2 for 1kHz PWM on PA0 (TIM2_CH1, AF1) */
void pwm_init(void) {
    RCC->AHB1ENR |= RCC_AHB1ENR_GPIOAEN;
    RCC->APB1ENR |= RCC_APB1ENR_TIM2EN;

    /* PA0 as AF1 (TIM2_CH1) */
    GPIOA->MODER &= ~(3U << 0);
    GPIOA->MODER |= (2U << 0);          /* alternate function */
    GPIOA->AFR[0] &= ~(0xFU << 0);
    GPIOA->AFR[0] |= (1U << 0);         /* AF1 */

    /* Timer: 84MHz / (8400+1) / (1000+1) = 10 Hz PWM? Recompute for 1kHz */
    TIM2->PSC = 83;                     /* /84 → 1 MHz */
    TIM2->ARR = 999;                    /* /1000 → 1 kHz */
    TIM2->CCR1 = 500;                   /* 50% duty cycle */

    /* PWM mode 1, channel 1 */
    TIM2->CCMR1 = (6U << TIM_CCMR1_OC1M_Pos) | TIM_CCMR1_OC1PE;
    TIM2->CCER = TIM_CCER_CC1E;         /* enable channel 1 output */
    TIM2->CR1 = TIM_CR1_CEN;            /* start */
}

/* TIM3 for 1ms periodic interrupt */
void tim3_init(void) {
    RCC->APB1ENR |= RCC_APB1ENR_TIM3EN;
    TIM3->PSC = 83;                     /* 1 MHz */
    TIM3->ARR = 999;                    /* 1 ms */
    TIM3->DIER |= TIM_DIER_UIE;         /* update interrupt enable */
    TIM3->CR1 |= TIM_CR1_CEN;
    NVIC_SetPriority(TIM3_IRQn, 5);
    NVIC_EnableIRQ(TIM3_IRQn);
}

volatile uint32_t g_ms_ticks = 0;
void TIM3_IRQHandler(void) {
    if (TIM3->SR & TIM_SR_UIF) {
        TIM3->SR &= ~TIM_SR_UIF;
        g_ms_ticks++;
    }
}
```

## Interrupts (NVIC)

```c
/* Priority grouping: preemption vs sub-priority */
NVIC_SetPriorityGrouping(NVIC_PRIORITYGROUP_4);  /* all bits preemption */

/* Set priorities — lower number = higher priority */
NVIC_SetPriority(USART2_IRQn, 6);   /* low priority */
NVIC_SetPriority(DMA2_Stream0_IRQn, 2);  /* high priority */
NVIC_SetPriority(SysTick_IRQn, 15);      /* lowest */

/* Enable */
NVIC_EnableIRQ(USART2_IRQn);
```

### Priority Rules

- **Highest priority** for time-critical ISRs (DMA completion, high-rate sensor reads)
- **Lowest priority** for non-critical ISRs (UART RX, slow sensors)
- **Never** do `printf` or `malloc` in an ISR
- **Keep ISRs < 50 microseconds**; defer all processing to tasks
- **Disable interrupts only briefly** — `__disable_irq()` / `__enable_irq()` pairs for atomic access

### Critical Sections

```c
/* Save and restore PRIMASK for nested critical sections */
uint32_t primask = __get_PRIMASK();
__disable_irq();
/* ... atomic operation ... */
if (!primask) {
    __enable_irq();
}
```

## DMA (Direct Memory Access)

DMA moves data between peripherals and memory without CPU intervention. Essential for high-throughput I/O.

```c
/* DMA2 Stream0 for ADC1 (circular buffer) */
void adc_dma_init(uint16_t *buffer, uint32_t len) {
    RCC->AHB1ENR |= RCC_AHB1ENR_DMA2EN;

    /* Disable stream before config */
    DMA2_Stream0->CR &= ~DMA_SxCR_EN;
    while (DMA2_Stream0->CR & DMA_SxCR_EN);

    /* Configure */
    DMA2_Stream0->PAR = (uint32_t)&ADC1->DR;        /* peripheral address */
    DMA2_Stream0->M0AR = (uint32_t)buffer;          /* memory address */
    DMA2_Stream0->NDTR = len;                       /* number of transfers */
    DMA2_Stream0->CR = DMA_SxCR_CHSEL_0             /* channel 0 (ADC1) */
                     | DMA_SxCR_MSIZE_0             /* 16-bit memory */
                     | DMA_SxCR_PSIZE_0             /* 16-bit peripheral */
                     | DMA_SxCR_MINC                /* memory increment */
                     | DMA_SxCR_CIRC                /* circular mode */
                     | DMA_SxCR_TCIE;               /* transfer complete IRQ */
    DMA2_Stream0->FCR = DMA_SxFCR_DMDIS | DMA_SxFCR_FTH;  /* full FIFO */

    NVIC_SetPriority(DMA2_Stream0_IRQn, 3);
    NVIC_EnableIRQ(DMA2_Stream0_IRQn);

    /* Start */
    DMA2_Stream0->CR |= DMA_SxCR_EN;
    ADC1->CR2 |= ADC_CR2_DMA | ADC_CR2_DDS | ADC_CR2_SWSTART;
}

void DMA2_Stream0_IRQHandler(void) {
    if (DMA2->LISR & DMA_LISR_TCIF0) {
        DMA2->LIFCR = DMA_LIFCR_CTCIF0;  /* clear flag */
        /* Buffer full — process or swap with a second buffer (double-buffering) */
    }
}
```

### DMA Patterns

| Pattern | Use |
|---|---|
| **Circular + half-transfer + transfer-complete IRQ** | Continuous ADC sampling with double-buffer |
| **Normal mode + TC IRQ** | One-shot transfer (e.g., SPI flash bulk read) |
| **Memory-to-memory** | Fast buffer copy (no peripheral involved) |
| **Double-buffering (M0AR + M1AR)** | Process one buffer while DMA fills the other |

## Hardware Abstraction Layer (HAL)

A good HAL hides MCU specifics behind a portable API:

```c
/* hal.h — portable interface */
typedef struct {
    void (*init)(void);
    uint16_t (*read)(uint8_t channel);
    void (*start_dma)(uint16_t *buf, uint32_t len);
} hal_adc_t;

extern const hal_adc_t hal_adc_stm32f4;
extern const hal_adc_t hal_adc_esp32;

/* Application code uses the interface, not the implementation */
extern const hal_adc_t *g_adc;  /* set at startup based on board */
```

For multi-MCU projects, prefer the **CMSIS** standard (Cortex-M) or use **Zephyr's device tree** for portability.

## Startup Code

Every MCU needs startup code that runs before `main()`:
1. Set the stack pointer (from vector table)
2. Copy `.data` section from flash to RAM
3. Zero the `.bss` section
4. Call `SystemInit()` (clocks, FPU enable)
5. Call C++ static constructors (if C++)
6. Call `main()`

This is usually provided by the vendor (e.g., `startup_stm32f407xx.s`) — don't write it yourself unless you understand it deeply.

## Linker Script Basics

```ld
/* Minimal linker script for Cortex-M */
ENTRY(Reset_Handler)

MEMORY {
    FLASH (rx)  : ORIGIN = 0x08000000, LENGTH = 512K
    RAM   (rwx) : ORIGIN = 0x20000000, LENGTH = 128K
}

_estack = ORIGIN(RAM) + LENGTH(RAM);

SECTIONS {
    .isr_vector : { KEEP(*(.isr_vector)) } > FLASH
    .text       : { *(.text*) *(.rodata*) } > FLASH
    .data       : { *(.data*) } > RAM AT > FLASH
    .bss        : { *(.bss*) *(COMMON) } > RAM
}
```

Track section sizes with `arm-none-eabi-size firmware.elf` — flash = text + data, RAM = data + bss.

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Forgot to enable peripheral clock | Register writes do nothing | Check `RCC->XXENR` first |
| Using `ODR |=` instead of `BSRR` | Race condition with ISR | Use `BSRR` (atomic) |
| ISR runs forever | Watchdog reset | Keep ISR short; no loops |
| Stack overflow | HardFault | Measure high-water; increase stack |
| Floating-point in ISR on non-FPU core | Exception | Check `SCB->CPACR`; use integer math |
| Wrong voltage on analog pin | Wrong ADC reading | Check VREF + GPIO analog mode |
| Not reading DR clears flag | RXNE never clears | Read DR to clear RXNE |
| Priority inversion | High-prio task blocked | Use mutex with priority inheritance |
| Uninitialized static C++ object | Constructor not called | Implement `__libc_init_array()` |
