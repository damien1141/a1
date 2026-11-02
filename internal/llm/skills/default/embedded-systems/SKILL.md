---
name: embedded-systems
description: Develops firmware for microcontrollers and resource-constrained devices — bare-metal C/C++ on ARM Cortex-M, RTOS applications (FreeRTOS, Zephyr), HAL/peripheral drivers, interrupt and DMA design, real-time deadlines, power management, memory optimization, and hardware/software co-design. Includes debugging (JTAG/SWD, logic analyzer, oscilloscope), safety standards (MISRA C, CERT C), and Rust embedded. Use when writing firmware, configuring peripherals, implementing interrupt handlers, optimizing power/memory, or debugging timing issues on STM32/ESP32/nRF/RP2040 and similar MCUs.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: systems
  triggers: embedded systems, firmware, microcontroller, MCU, RTOS, FreeRTOS, Zephyr, STM32, ESP32, nRF52, RP2040, bare metal, HAL, interrupt, ISR, DMA, real-time, power management, MISRA C, CERT C, JTAG, SWD, logic analyzer, Rust embedded
  role: specialist
  scope: implementation
  output-format: code
  related-skills: cpp-pro, rust-pro, debugging-wizard, testing-master
---

# Embedded Systems

Firmware for resource-constrained devices where every byte, microsecond, and microamp counts. Spans bare-metal C/C++ on ARM Cortex-M, RTOS-based designs (FreeRTOS, Zephyr), peripheral driver development, real-time scheduling, power management, memory optimization, hardware/software co-design, and safety-critical coding standards. Verification is not optional — a missed deadline or a stack overflow on a device in the field is a recall.

## When to Use

- Writing firmware for microcontrollers (STM32, ESP32, nRF52, RP2040, AVR, etc.)
- Configuring peripherals (GPIO, ADC, UART, SPI, I2C, CAN, timers, PWM)
- Implementing interrupt handlers (ISRs) and DMA transfers
- Building RTOS applications (FreeRTOS, Zephyr, ThreadX)
- Optimizing power consumption (sleep modes, low-power design)
- Sizing flash/RAM/stack under tight constraints
- Debugging timing issues, race conditions, or hardware-software bugs
- Porting between MCUs or building a hardware abstraction layer (HAL)
- Meeting safety standards (MISRA C, CERT C, IEC 61508)
- Evaluating Rust for embedded (no_std, embedded-hal)

## Operating Loop

1. **Analyze constraints** — MCU specs (core, clocks, memory), peripheral needs, timing deadlines, power budget, environmental. **Gate:** constraints documented; memory budget table written.
2. **Choose architecture** — bare-metal superloop vs RTOS vs Zephyr. Plan task structure, interrupt priorities, peripheral allocation, memory map. **Gate:** architecture sketch with task/interrupt diagram; stack sizes estimated.
3. **Implement drivers** — HAL, peripheral drivers, RTOS integration. Always from datasheet first; verify register bit-fields against the reference manual. **Gate:** `arm-none-eabi-gcc -Wall -Werror -Wextra` clean; `cppcheck` clean.
4. **Validate on hardware** — Compile, flash, verify with logic analyzer or oscilloscope. Check ISR latency, stack high-water mark, no missed deadlines under worst-case load. **Gate:** timing assertions pass; stack high-water > 32 words; ISR latency < budget.
5. **Optimize** — Code size (flash), RAM usage, power consumption. Profile before optimizing. **Gate:** resource usage table updated; meets power/size budget.
6. **Harden** — Watchdog, brown-out detection, fault handlers, assert in critical paths, fuzz inputs. **Gate:** watchdog kicks in all tasks; fault handler dumps state; BOR enabled.
7. **Test long-run** — Soak test for hours; stress-test edge cases (brown-out, ESD, brown-in, voltage spikes). **Gate:** 24-hour soak stable; no crashes; power-cycling 1000x stable.

## Architecture Choice

| Need | Choice |
|---|---|
| Single foreground loop, no hard deadlines | Bare-metal superloop |
| Multiple concurrent activities with deadlines | RTOS (FreeRTOS / Zephyr) |
| Wireless (BLE, Thread, Wi-Fi) | Zephyr (BLE mesh), ESP-IDF (Wi-Fi), nRF Connect SDK |
| Safety-critical, certification target | RTOS with certification (SafeRTOS, ThreadX) + MISRA C |
| Lots of drivers / ecosystem | Zephyr (200+ board configs, device tree) |
| Tiny (<32KB flash, <8KB RAM) | Bare-metal or minimal RTOS (FreeRTOS) |
| Rust preference | `embassy` (async) or `embedded-hal` ecosystem |
| Rapid prototyping | Arduino framework (then port) |

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| Microcontroller programming | `references/microcontroller-programming.md` | Bare-metal, registers, peripherals, GPIO/ADC/timers, interrupts |
| RTOS patterns | `references/rtos-patterns.md` | FreeRTOS / Zephyr tasks, queues, semaphores, scheduling, priority inversion |
| Communication protocols | `references/communication-protocols.md` | I2C, SPI, UART, CAN, 1-Wire implementation patterns |
| Power management | `references/power-management.md` | Sleep modes, low-power design, battery life, wake sources |
| Memory & performance | `references/memory-optimization.md` | Code size, RAM/stack management, flash optimization, linker scripts |
| Debugging & safety | `references/debugging-safety.md` | JTAG/SWD, logic analyzer, oscilloscope, MISRA C, CERT C, Rust embedded |

## Code Examples

### Minimal ISR (ARM Cortex-M / STM32 HAL)

```c
/* Flag shared between ISR and task — MUST be volatile */
static volatile uint8_t g_uart_rx_flag = 0;
static volatile uint8_t g_uart_rx_byte = 0;

/* Keep ISR short: read hardware, set flag, exit. Defer work to a task. */
void USART2_IRQHandler(void) {
    if (USART2->SR & USART_SR_RXNE) {
        g_uart_rx_byte = (uint8_t)(USART2->DR & 0xFF);  /* reading DR clears RXNE */
        g_uart_rx_flag = 1;
    }
}

void process_uart(void) {
    if (g_uart_rx_flag) {
        __disable_irq();                /* enter critical section */
        uint8_t byte = g_uart_rx_byte;
        g_uart_rx_flag = 0;
        __enable_irq();                 /* exit critical section */
        handle_byte(byte);
    }
}
```

### FreeRTOS periodic task with deadline

```c
static void vSensorTask(void *pvParameters) {
    TickType_t xLastWakeTime = xTaskGetTickCount();
    const TickType_t xPeriod = pdMS_TO_TICKS(10);   /* 10ms period */

    for (;;) {
        uint16_t raw = adc_read_channel(ADC_CH0);
        xQueueSend(xSensorQueue, &raw, 0);          /* non-blocking send */

        /* Stack high-water check in debug builds */
        configASSERT(uxTaskGetStackHighWaterMark(NULL) > 32);

        vTaskDelayUntil(&xLastWakeTime, xPeriod);   /* precise periodic wake */
    }
}
```

### Bare-metal timer + GPIO blink (STM32F4)

```c
#include "stm32f4xx.h"

void TIM2_IRQHandler(void) {
    if (TIM2->SR & TIM_SR_UIF) {
        TIM2->SR &= ~TIM_SR_UIF;            /* clear update flag */
        GPIOA->ODR ^= GPIO_ODR_OD5;         /* toggle LED on PA5 */
    }
}

void blink_init(void) {
    RCC->AHB1ENR |= RCC_AHB1ENR_GPIOAEN;
    GPIOA->MODER |= GPIO_MODER_MODER5_0;    /* PA5 output */

    RCC->APB1ENR |= RCC_APB1ENR_TIM2EN;
    TIM2->PSC  = 8399;                       /* /8400 → 10 kHz */
    TIM2->ARR  = 9999;                       /* /10000 → 1 Hz */
    TIM2->DIER |= TIM_DIER_UIE;
    TIM2->CR1  |= TIM_CR1_CEN;

    NVIC_SetPriority(TIM2_IRQn, 6);
    NVIC_EnableIRQ(TIM2_IRQn);
}
```

### Rust embedded (no_std, embassy async)

```rust
#![no_std]
#![no_main]

use embassy_executor::Spawner;
use embassy_stm32::gpio::{Level, Output, Speed};
use embassy_time::Timer;

#[embassy_executor::main]
async fn main(_spawner: Spawner) {
    let p = embassy_stm32::init(Default::default());
    let mut led = Output::new(p.PA5, Level::High, Speed::Low);
    loop {
        led.set_high();
        Timer::after_millis(500).await;
        led.set_low();
        Timer::after_millis(500).await;
    }
}
```

### Verification gates

```bash
# Build with strict warnings (no warnings allowed in production firmware)
arm-none-eabi-gcc -Wall -Werror -Wextra -Wconversion -std=c17 -Os \
    -mcpu=cortex-m4 -mthumb -mfpu=fpv4-sp-d16 -mfloat-abi=hard \
    -ffunction-sections -fdata-sections \
    -T linker.ld -o firmware.elf src/*.c

# Static analysis
cppcheck --enable=all --suppress=missingInclude --error-exitcode=1 src/

# Size report (track flash/RAM)
arm-none-eabi-size firmware.elf
arm-none-eabi-nm -S --size-sort firmware.elf | tail -20

# Rust embedded (separate toolchain)
rustup target add thumbv7em-none-eabi
cargo build --target thumbv7em-none-eabi --release
cargo size --target thumbv7em-none-eabi --release
```

## Constraints

### MUST DO
- Use `volatile` for all hardware registers and ISR-shared variables
- Keep ISRs short — read hardware, set flag, exit; defer work to tasks
- Configure the watchdog in every task; kick it always
- Enable brown-out detection (BOR) — random resets are worse than clean resets
- Define stack sizes per task; check `uxTaskGetStackHighWaterMark()` in debug builds
- Protect shared resources with critical sections or RTOS primitives
- Use CMSIS / vendor HAL for portability; don't bit-bang if a peripheral exists
- Read the datasheet and errata sheet — every MCU has known bugs
- Verify register bit-field definitions against the reference manual (vendor headers drift)
- Compile with `-Wall -Werror -Wextra`; run `cppcheck` in CI
- Set interrupt priorities carefully; never do priority inversion unintentionally
- Pin toolchain versions (GCC, Arm clang) — firmware is sensitive to compiler changes
- Plan for brown-out, power glitches, ESD; design fault handlers that dump state
- Document memory budget (flash, RAM, stack) per build

### MUST NOT DO
- Block in ISRs (no `while(!flag)` loops, no `printf`, no `malloc`)
- Use `malloc`/`free` in embedded (use static allocation or pool allocators)
- Access shared resources without synchronization
- Use floating-point without checking the MCU has an FPU (or use soft-float)
- Hardcode hardware-specific values (clocks, addresses) — use vendor headers
- Ignore hardware errata (STM32F4 silicon rev A vs Z, ESP32 v0 vs v1)
- Disable interrupts for long stretches (kills real-time response)
- Trust `printf` for production debugging — use ITM, semihosting, or a UART log
- Skip the watchdog on long-running tasks
- Mix signed/unsigned arithmetic without `-Wconversion`
- Assume `int` is 32-bit (ILP32 vs LP64 matters on Cortex-M)
- Allocate stack without measuring (use high-water mark)
- Use recursion on embedded (unbounded stack growth)
- Forget to handle fault handlers (HardFault, MemManage, BusFault, UsageFault)

## Output Template

When delivering embedded work, provide:

1. **Constraints summary** — MCU, clocks, flash/RAM budget, power budget, deadline table
2. **Architecture** — task/interrupt diagram, peripheral allocation, memory map
3. **Driver implementation** — HAL, peripheral drivers, ISR handlers
4. **Application code** — RTOS tasks or main loop with state machines
5. **Resource usage** — flash/RAM/stack actual vs budget; power estimate
6. **Verification** — build commands (`-Wall -Werror`), static analysis, timing measurements, stack high-water, 24h soak results
7. **Risk register** — errata workarounds, known timing margins, ESD/power considerations

## Knowledge Reference

**Architectures:** ARM Cortex-M (M0/M0+/M3/M4/M7/M23/M33), ESP32 (Xtensa + RISC-V), RISC-V (GD32V, CH32V), AVR, RP2040 (Cortex-M0+ dual-core). **RTOS:** FreeRTOS (tasks, queues, semaphores, mutexes, timers), Zephyr (device tree, 200+ board configs, BLE mesh, TF-M), ThreadX, embOS. **Peripherals:** GPIO, ADC/DAC, UART, SPI, I2C, CAN, 1-Wire, timers/PWM, QSPI, USB, Ethernet, DMA. **Power:** sleep modes (sleep/stop/standby/shutdown), low-power UART/LPTIM, RTC wake, brown-out. **Memory:** linker scripts, scatter files, ITCM/DTCM/AXI SRAM, flash sectors, external QSPI. **Debugging:** JTAG/SWD, OpenOCD, ST-Link, J-Link, ITM/SWO trace, logic analyzer (Saleae, Sigrok), oscilloscope, fault handlers. **Safety:** MISRA C:2012 (mandatory/required/advisory rules), CERT C (preprocessor, integers, memory, strings), IEC 61508 (SIL), ISO 26262 (ASIL), DO-178C. **Rust embedded:** `no_std`, `embedded-hal` traits, `cortex-m` crate, `embassy` (async executor), `probe-rs` for flashing/debugging.
