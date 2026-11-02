# Memory & Performance Reference

Code size, RAM/stack management, flash optimization, and linker script patterns for resource-constrained MCUs.

## Memory Map (Cortex-M)

```
Typical STM32F4:
  Flash: 0x08000000 – 0x0807FFFF (512KB)
  SRAM:  0x20000000 – 0x2001FFFF (128KB)
  CCM:   0x10000000 – 0x1000FFFF (64KB) — core-coupled, fast, no DMA access
  Peripherals: 0x40000000 – 0x40023BFF
```

Check the datasheet — your specific MCU has specific regions (ITCM, DTCM, AXI SRAM, etc.).

## Sizing with `arm-none-eabi-size`

```bash
$ arm-none-eabi-size firmware.elf
   text    data     bss     dec     hex filename
  45280    1232   16384   62896    f5b0 firmware.elf
```

- **text** = code + rodata → goes in **flash**
- **data** = initialized variables → flash copy + RAM
- **bss** = zero-initialized variables → **RAM** only

**Flash used** = text + data
**RAM used** = data + bss + stack + heap

Track these in CI. Fail the build if they exceed budget.

## Stack Sizing

```c
/* FreeRTOS: check at runtime */
UBaseType_t high_water = uxTaskGetStackHighWaterMark(NULL);
/* high_water is in WORDS. If < 32, increase stack size. */

/* Bare-metal: paint the stack with a pattern, check after running */
#define STACK_FILL  0xDEADBEEF
extern uint32_t _estack, _sstack;

void stack_paint(void) {
    for (uint32_t *p = &_sstack; p < &_estack - 64; p++) {
        *p = STACK_FILL;
    }
}

uint32_t stack_high_water(void) {
    uint32_t *p = &_sstack;
    while (*p == STACK_FILL && p < &_estack) p++;
    return (uint32_t)(&_estack - p) * 4;  /* bytes used */
}
```

**Rule:** set stack size = max measured + 50%. For FreeRTOS tasks, minimum 256 words (1KB) even for "trivial" tasks — interrupts and library calls use the task stack.

## Linker Script Patterns

```ld
/* linker.ld — production-grade for STM32F4 */
ENTRY(Reset_Handler)

MEMORY {
    FLASH (rx)  : ORIGIN = 0x08000000, LENGTH = 512K
    RAM   (rwx) : ORIGIN = 0x20000000, LENGTH = 128K
    CCM   (rwx) : ORIGIN = 0x10000000, LENGTH = 64K   /* core-coupled */
}

_estack = ORIGIN(RAM) + LENGTH(RAM);

SECTIONS {
    .isr_vector : {
        . = ALIGN(4);
        KEEP(*(.isr_vector))
        . = ALIGN(4);
    } > FLASH

    .text : {
        . = ALIGN(4);
        *(.text*)            /* code */
        *(.rodata*)          /* strings, const data */
        *(.glue_7) *(.glue_7t)
        *(.eh_frame)
        KEEP(*(.init))
        KEEP(*(.fini))
        . = ALIGN(4);
        _etext = .;
    } > FLASH

    /* ARM exception unwinding tables */
    .ARM.extab : { *(.ARM.extab* .gnu.linkonce.armextab.*) } > FLASH
    .ARM : {
        __exidx_start = .;
        *(.ARM.exidx*)
        __exidx_end = .;
    } > FLASH

    .data : {
        . = ALIGN(4);
        _sdata = .;
        *(.data*)
        . = ALIGN(4);
        _edata = .;
    } > RAM AT > FLASH
    _sidata = LOADADDR(.data);

    .bss (NOLOAD) : {
        . = ALIGN(4);
        _sbss = .;
        *(.bss*) *(COMMON)
        . = ALIGN(4);
        _ebss = .;
    } > RAM

    /* Heap grows up from end of bss; stack grows down from top of RAM */
    . = ALIGN(8);
    end = .;
    _end = .;
    PROVIDE(end = .);
    PROVIDe(_end = .);
}
```

### CCM RAM (STM32F4-specific)

CCM is core-coupled — 0 wait states, fastest execution. But **DMA can't access it**. Put critical code/ISR stacks here:

```c
/* Put a function in CCM */
__attribute__((section(".ccmtext"))) void fast_func(void) { ... }

/* Put a buffer in CCM (NOT DMA-able!) */
__attribute__((section(".ccmram"))) uint32_t fast_buf[256];
```

## Compiler Optimization Flags

```makefile
CFLAGS = -Os \                    # optimize for size (or -O2 for speed)
         -ffunction-sections \    # enable GC of unused functions
         -fdata-sections \        # enable GC of unused data
         -flto \                  # link-time optimization (5-15% size reduction)
         -mcpu=cortex-m4 -mthumb -mfpu=fpv4-sp-d16 -mfloat-abi=hard \
         -fno-exceptions \        # no C++ exceptions
         -fno-rtti \              # no C++ RTTI
         -fno-unwind-tables \     # no exception unwinding tables
         -fno-asynchronous-unwind-tables \
         -ffast-math \            # faster float (relaxed IEEE)
         --specs=nano.specs \     # newlib-nano (much smaller printf)
         --specs=nosys.specs      # no syscalls stubs

LDFLAGS = -Wl,--gc-sections \     # remove unused sections
          -Wl,--print-memory-usage \  # show flash/RAM use at link
          -Wl,-Map=firmware.map   # generate map file for analysis
```

### `newlib-nano`

The `--specs=nano.specs` flag switches to newlib-nano, which is dramatically smaller for `printf`/`scanf` (often 10-50KB savings). Always use it on embedded.

## Analyzing Binary Size

```bash
# Map file analysis: what's taking space?
arm-none-eabi-size -A firmware.elf | sort -k2 -n -r | head -20

# Symbols sorted by size
arm-none-eabi-nm -S --size-sort firmware.elf | tail -30

# Per-file size
arm-none-eabi-size -A firmware.elf | grep -E '\.o|\.a' | sort -k2 -n -r
```

### Reducing Code Size

| Technique | Savings | Cost |
|---|---|---|
| `-Os` (size opt) | 10-20% vs -O2 | Slight speed loss |
| `-flto` (LTO) | 5-15% | Slower build |
| newlib-nano | 10-50KB | No floats in printf unless `_printf_float` |
| `-fno-exceptions` (C++) | Big | No exceptions |
| `-fno-rtti` (C++) | ~10KB | No `dynamic_cast` |
| Remove unused features (e.g., disable BLE) | Huge | Loss of feature |
| Inline small functions | varies | May grow if overused |
| Custom `malloc` (pool allocator) | ~5KB | No free() of arbitrary sizes |
| Avoid `printf` (use itoa + write) | 5-20KB | Less convenient |

## Custom `printf` Replacement

For tight flash budgets, skip `printf`:

```c
/* Minimal logging — way smaller than printf */
void log_str(const char *s) {
    while (*s) {
        while (!(USART2->SR & USART_SR_TXE));
        USART2->DR = *s++;
    }
}

void log_uint(uint32_t v) {
    char buf[12];
    int i = 11;
    buf[i--] = 0;
    if (v == 0) { buf[i--] = '0'; }
    while (v > 0) {
        buf[i--] = '0' + (v % 10);
        v /= 10;
    }
    log_str(&buf[i + 1]);
}

#define LOG(msg) do { log_str("[" __FILE__ "] "); log_str(msg); log_str("\n"); } while (0)
```

## Static Allocation (No malloc)

Dynamic allocation on embedded is risky:
- Fragmentation over time
- Allocation can fail unexpectedly
- `malloc`/`free` are non-deterministic (bad for hard real-time)
- FreeRTOS heap is a fixed buffer, but you still need to handle allocation failures

```c
/* WRONG — dynamic allocation in firmware */
void process(void) {
    uint8_t *buf = malloc(256);   // may fail; fragments heap
    /* ... */
    free(buf);
}

/* RIGHT — static allocation */
static uint8_t buf[256];  /* zero-BSS, no runtime cost */
void process(void) {
    /* ... use buf ... */
}

/* RIGHT — pool allocator for fixed-size objects */
typedef struct { /* ... */ } Event_t;
static Event_t event_pool[32];
static uint32_t event_used = 0;
Event_t *event_alloc(void) {
    for (int i = 0; i < 32; i++) {
        if (!(event_used & (1U << i))) {
            event_used |= (1U << i);
            return &event_pool[i];
        }
    }
    return NULL;
}
```

## Memory Placement Tricks

```c
/* Critical ISR code in RAM (faster on MCUs with slow flash wait states) */
__attribute__((section(".ramfunc"), long_call))
void fast_isr(void) { ... }

/* DMA buffers MUST be in DMA-accessible RAM (not CCM on STM32F4) */
__attribute__((aligned(32)))  /* cache-line aligned for DMA */
uint8_t dma_buffer[1024];

/* Constant lookup table in flash (saves RAM) */
const uint16_t sin_table[256] = { /* ... */ };  /* stored in flash, not copied to RAM */

/* vs (WRONG) — copy to RAM: */
uint16_t sin_table[256] = { /* ... */ };  /* wastes 512 bytes RAM */
```

## Cache Considerations (Cortex-M7)

M7 has data and instruction caches. For DMA:

```c
/* Before reading DMA-written data: clean D-cache */
SCB_CleanDCache_by_Addr((uint32_t *)buf, sizeof(buf));

/* Before letting DMA read CPU-written data: invalidate D-cache */
SCB_InvalidateDCache_by_Addr((uint32_t *)buf, sizeof(buf));

/* Or: configure MPU to make DMA buffer region non-cacheable */
```

Forgetting cache operations on M7 = silent data corruption.

## Performance Profiling

```c
/* Cycle counter (Cortex-M3+) */
#define DEMCR (*(volatile uint32_t *)0xE000EDFC)
#define DWT_CYCCNT (*(volatile uint32_t *)0xE0001004)

void profile_init(void) {
    DEMCR |= (1 << 24);     /* TRCENA */
    DWT_CYCCNT = 0;
    *(volatile uint32_t *)0xE0001000 |= 1;  /* enable counter */
}

uint32_t profile(uint32_t prev) {
    return DWT_CYCCNT - prev;
}

/* Usage */
uint32_t t0 = DWT_CYCCNT;
expensive_function();
uint32_t elapsed = profile(t0);
log_uint(elapsed);  /* cycles — divide by clock speed for seconds */
```

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Stack overflow | HardFault | Measure high-water; increase stack |
| DMA buffer in CCM (STM32F4) | DMA fails | Use main SRAM for DMA buffers |
| Forgetting `--gc-sections` | Bloated binary | Always use with `-ffunction-sections` |
| `printf` with `%f` | +20KB flash | Use newlib-nano; avoid float printf |
| Static const in RAM | Wastes RAM | Mark `const` → goes to flash |
| Cache incoherence (M7) | Silent corruption | Clean/invalidate D-cache around DMA |
| Unaligned access | HardFault on Cortex-M0 | Use `__packed` or align structs |
| Recursion | Stack overflow | Avoid; use iteration |
| Global uninitialized state | Random behavior | Initialize explicitly in `main` |
