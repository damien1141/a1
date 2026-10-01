# Debugging & Safety Reference

Debugging tools (JTAG/SWD, logic analyzer, oscilloscope) and safety standards (MISRA C, CERT C) for production firmware. Plus Rust embedded.

## Debug Interfaces

### SWD (Serial Wire Debug)

Standard for Cortex-M. Two pins: SWDIO + SWCLK (+ optional SWO for trace). Replaces JTAG (5 pins).

```bash
# OpenOCD with ST-Link
openocd -f interface/stlink.cfg -f target/stm32f4x.cfg

# In another terminal: telnet localhost 4444
> reset halt
> flash write_image erase firmware.elf
> reset run

# GDB
arm-none-eabi-gdb firmware.elf
(gdb) target remote :3333
(gdb) load
(gdb) monitor reset halt
(gdb) b main
(gdb) continue
```

### JTAG

Older standard, 4 pins (TCK/TMS/TDI/TDO) + reset. Used on bigger chips (Cortex-A, some older MCUs).

### Debug Probes

| Probe | Price | Best for |
|---|---|---|
| **ST-Link v2/v3** | $25 / $80 | STM32; cheap; widely supported |
| **J-Link EDU/ BASE** | $60 / $400 | All ARM; excellent support; fast |
| **DAPLink** | $15 | ARM mbed; cheap; CMSIS-DAP |
| **Picoprobe** | $5 | Use a Raspberry Pi Pico as a SWD probe |
| **probe-rs** (software) | Free | Modern Rust-based debugger; multi-arch |

## ITM / SWO Trace

ITM (Instrumentation Trace Macrocell) lets you `printf` over a single SWO pin without slowing the CPU:

```c
/* ITM printf */
#define ITM_Port8(n)    (*((volatile uint8_t *)(0xE0000000 + 4*n)))
#define ITM_Port32(n)   (*((volatile uint32_t *)(0xE0000000 + 4*n)))
#define DEMCR           (*((volatile uint32_t *)0xE000EDFC))

void itm_send_char(char c) {
    if (DEMCR & (1 << 24)) {        /* TRCENA */
        while (ITM_Port32(0) == 0); /* wait for ITM ready */
        ITM_Port8(0) = c;
    }
}

void itm_printf(const char *s) {
    while (*s) itm_send_char(*s++);
}
```

View ITM output in:
- **STM32CubeIDE** — SWV ITM Data Console
- **Orbtrace** — open-source ITM decoder
- **probe-rs** — `probe-rs dbg --chip stm32f4x` + ITM trace

## Logic Analyzer

Essential for protocol debugging. Cheap options:
- **Saleae Logic** ($200+; gold standard)
- **DSLogic** ($99)
- **Sigrok + any cheap analyzer** ($10)
- **Pi Pico as logic analyzer** (5 channels, 100MHz, $5)

### Pattern: Decode the Bus

1. Connect probes to the bus (SCL/SDA, MOSI/MISO/SCK/CS, etc.)
2. Trigger on the start condition (CS falling edge, I2C START)
3. Capture 10+ transactions
4. Use protocol decoder in PulseView/Saleae
5. Compare decoded bytes against expected

**Rule:** If the protocol "isn't working," the logic analyzer is your first stop. It will show you whether the MCU is sending the right bytes, the peripheral is ACKing, and timing is correct.

### Common Logic Analyzer Findings

- Wrong baud rate (UART bytes look like garbage)
- CS not toggling (SPI slave not selected)
- Missing pull-ups (I2C stuck low)
- Clock polarity wrong (SPI sample on wrong edge)
- Bus stuck (a slave holding SDA low after a reset)

## Oscilloscope

For analog / timing issues the logic analyzer can't see:
- Signal integrity (ringing, overshoot)
- Power supply noise
- Crystal oscillator startup
- Edge rates

Cheap options: **Rigol DS1054Z** ($400), **Hantek 6022BE** ($100), **Pi Pico as scope** (limited).

## Fault Handlers

When the MCU faults (HardFault, MemManage, BusFault, UsageFault), it should dump state for debugging:

```c
void HardFault_Handler(void) __attribute__((naked));
void HardFault_Handler(void) {
    __asm volatile(
        "tst lr, #4                              \n"
        "ite eq                                  \n"
        "mrseq r0, msp                           \n"  /* use MSP */
        "mrsne r0, psp                           \n"  /* use PSP */
        "b HardFault_Handler_C                   \n"
    );
}

void HardFault_Handler_C(uint32_t *stack) {
    uint32_t r0, r1, r2, r3, r12, lr, pc, psr;
    r0  = stack[0]; r1  = stack[1]; r2  = stack[2]; r3  = stack[3];
    r12 = stack[4]; lr  = stack[5]; pc = stack[6]; psr = stack[7];

    /* Log fault state (write to non-volatile log or ITM) */
    log_fault(PC=pc, LR=lr, PSR=psr, BFSR=SCB->CFSR, HFSR=SCB->HFSR);

    /* Optional: reset after a delay */
    NVIC_SystemReset();
}
```

Decoding the fault address tells you what code caused the crash. Combine with the `.map` file to find the function.

## Static Analysis

### cppcheck

```bash
cppcheck --enable=all --suppress=missingInclude \
         --inline-suppr \
         --error-exitcode=1 \
         --check-config \
         -I include -I Drivers/CMSIS/Device/ST/STM32F4xx/Include \
         src/
```

Add `// cppcheck-suppress` comments for false positives. Don't ignore real warnings.

### clang-tidy

```bash
clang-tidy --checks='-*,bugprone-*,cert-*,cppcoreguidelines-*,misc-*,modernize-*,performance-*,readability-*' \
           -header-filter=.* \
           src/*.c -- -target arm-none-eabi -I include
```

### MISRA C:2012

MISRA C is a set of safety-critical C coding rules. Categories:
- **Mandatory** (10 rules) — must follow; non-compliance needs formal deviation
- **Required** (120+ rules) — should follow; deviations need justification
- **Advisory** (80+ rules) — recommended

Examples:

| Rule | Category | What |
|---|---|---|
| Rule 8.4 | Required | Compatible declarations of external objects |
| Rule 8.7 | Advisory | File-scope objects should be static if only used in one file |
| Rule 9.1 | Mandatory | Auto variables must be initialized before use |
| Rule 11.5 | Advisory | No cast from pointer-to-void to pointer-to-object |
| Rule 12.5 | Mandatory | `malloc`/`free` shall not be used |
| Rule 17.3 | Required | Function declared implicitly not allowed |
| Rule 17.7 | Required | Return value of functions must be used |
| Rule 21.3 | Required | Functions from `stdlib.h` (`malloc`/`calloc`/`realloc`/`free`) shall not be used |

Tools that enforce MISRA C: **PC-lint Plus**, **Coverity**, **Helix QAC**, **Compiler Explorer (gcc with -fanalyzer)**.

### CERT C

CERT C is a security-focused subset (vs MISRA's safety focus). Categories:
- **Preprocessor (PRE)** — macro safety
- **Integers (INT)** — overflow, signedness
- **Memory (MEM)** — buffer overflows, use-after-free
- **Strings (STR)** — strlen, strcpy safety
- **Miscellaneous (MSC)** — assertions, random

Example rule:
- **INT30-C** — No unsigned integer wraparound
- **MEM30-C** — Don't access freed memory
- **STR31-C** — Guarantee null-terminated strings

## Watchdog

```c
/* Independent Watchdog (IWDG) — runs on LSI, independent of main clock */
void iwdg_init(uint32_t timeout_ms) {
    IWDG->KR = 0xCCCC;            /* enable IWDG */
    IWDG->KR = 0x5555;            /* enable write access */
    /* LSI = 32kHz, prescaler /256 → 125 Hz → 8ms/tick */
    IWDG->PR = 6;                 /* /256 */
    IWDG->RLR = timeout_ms / 8;   /* timeout in ticks */
    while (IWDG->SR);             /* wait for registers to update */
    IWDG->KR = 0xAAAA;            /* reload (kick) */
}

/* Kick the dog frequently from your main loop / RTOS tasks */
void iwdg_kick(void) {
    IWDG->KR = 0xAAAA;
}
```

**Rule:** every long-running task must kick the watchdog. If any task hangs, the watchdog resets the system. For RTOS designs, use a watchdog task that monitors all other tasks' heartbeats.

## Rust Embedded

Rust is increasingly viable for embedded. The `embedded-hal` trait ecosystem + `probe-rs` tooling make it competitive.

### Toolchain Setup

```bash
# Install Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

# Add embedded targets
rustup target add thumbv7em-none-eabi      # Cortex-M4F
rustup target add thumbv7em-none-eabihf    # Cortex-M4F hard-float
rustup target add thumbv6m-none-eabi       # Cortex-M0+

# Install probe-rs (flash + debug)
cargo install probe-rs-tools

# Install flip-link (stack-overflow protection)
cargo install flip-link
```

### Project Structure

```bash
cargo generate --git https://github.com/rust-embedded/cortex-m-quickstart
```

### Blink LED (no_std)

```rust
#![no_std]
#![no_main]

use panic_halt as _;
use cortex_m_rt::entry;
use stm32f4xx_hal as hal;
use hal::{pac, prelude::*};

#[entry]
fn main() -> ! {
    let dp = pac::Peripherals::take().unwrap();
    let gpioc = dp.GPIOC.split();
    let mut led = gpioc.pc13.into_push_pull_output();

    loop {
        led.set_high();
        cortex_m::asm::delay(8_000_000);
        led.set_low();
        cortex_m::asm::delay(8_000_000);
    }
}
```

### Async with Embassy

```rust
#![no_std]
#![no_main]

use embassy_executor::Spawner;
use embassy_stm32::gpio::{Level, Output, Speed};
use embassy_time::Timer;

#[embassy_executor::main]
async fn main(_spawner: Spawner) {
    let p = embassy_stm32::init(Default::default());
    let mut led = Output::new(p.PC13, Level::High, Speed::Low);
    loop {
        led.toggle();
        Timer::after_millis(500).await;
    }
}
```

### Flash + Debug

```bash
cargo build --release
probe-rs run --chip STM32F407VGTx target/thumbv7em-none-eabihf/release/firmware
# Or GDB:
probe-rs gdb --chip STM32F407VGTx &
arm-none-eabi-gdb target/thumbv7em-none-eabihf/release/firmware
```

### Why Rust for Embedded

- **Memory safety** without GC — no use-after-free, no buffer overflows (compile-time checks)
- **`no_std`** works without an allocator
- **Fearless concurrency** — `Send`/`Sync` enforced at compile time
- **Strong typing** — units of measure, state machines as types
- **Tooling** — `cargo`, `clippy`, `rustfmt` are first-class
- **`embedded-hal` traits** — portable drivers across MCUs

### Caveats

- Smaller ecosystem than C (growing fast)
- Some vendor HALs less mature (STM32 is good; ESP32 is good; others vary)
- Code size slightly larger than optimized C (usually 10-20%)
- Binary compile times longer than C
- Certification: Rust + MISRA C is harder (MISRA is C-focused); see **MISRA C++** or **ferrocene** (Rust for safety-critical)

## Common Debugging Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Optimization removes variables | GDB shows "optimized out" | Build with -O0 or -Og for debug |
| Can't connect probe | "No target found" | Check wiring; check SWD pins not used as GPIO |
| Reset but doesn't run | Stuck in reset | Check BOOT0/BOOT1 pins; check reset line |
| Works in debug, breaks in release | Race condition | Add memory barriers; check ISR priorities |
| Fault in random places | Stack overflow | Increase stack; check high-water mark |
| Random resets | Brown-out, ESD | Enable BOR; check power supply |
