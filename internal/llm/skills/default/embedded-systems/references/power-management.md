# Power Management Reference

Sleep modes, wake sources, low-power design, and battery life estimation. Critical for battery-powered and energy-harvesting devices.

## Power Budget First

Before writing any code, calculate the power budget:

```
Battery life (hours) = battery_capacity_mAh / average_current_mA

Example: 1000mAh coin cell, 5mA average → 200 hours (~8 days)
```

**Measure** the actual current with a current analyzer or multimeter (μA range). Estimates are always wrong.

## STM32 Power Modes

| Mode | Current (typ) | Wake time | Wake source | RAM retained |
|---|---|---|---|---|
| **Run** | 50–100 mA | — | — | Yes |
| **Sleep** | ~10 mA | ~6 µs | Any IRQ | Yes |
| **Stop** | ~100 µA | ~10 µs | EXTI, RTC | Yes |
| **Standby** | ~3 µA | ~ms | WKUP pin, RTC | No (only backup) |
| **Shutdown** (M4+) | ~0.5 µA | ~ms | WKUP pin, RTC | No |

Trade-off: deeper sleep = lower power, but slower wake + less state retained.

## Stop Mode (the workhorse)

For periodic sensor reads, Stop mode + RTC wake is the sweet spot:

```c
#include "stm32f4xx.h"

void enter_stop_mode(void) {
    /* Configure all GPIO to analog (saves power) */
    /* ... or use the GPIO clock gating tricks ... */

    /* Enable PWR clock */
    RCC->APB1ENR |= RCC_APB1ENR_PWREN;

    /* Voltage regulator in low-power mode */
    PWR->CR |= PWR_CR_LPDS;

    /* Set SLEEPDEEP bit + clear PDDS (stop mode, not standby) */
    SCB->SCR |= SCB_SCR_SLEEPDEEP_Msk;
    PWR->CR &= ~PWR_CR_PDDS;

    /* Request Wait For Interrupt */
    __WFI();

    /* Wakes up here after RTC or EXTI */
    /* Re-enable clocks (Stop mode disables them) */
    SystemClock_Config();
    restore_gpio_state();
}
```

## RTC Wake from Stop

```c
void rtc_set_wakeup(uint32_t seconds) {
    RCC->APB1ENR |= RCC_APB1ENR_PWREN;
    PWR->CR |= PWR_CR_DBP;          /* disable RTC write protection */

    /* LSE oscillator (32.768 kHz) — must be enabled in RCC */
    RCC->BDCR |= RCC_BDCR_RTCEN | RCC_BDCR_RTCSEL_LSE;

    RTC->WPR = 0xCA;                /* unlock RTC write protection */
    RTC->WPR = 0x53;

    RTC->CR &= ~RTC_CR_WUTE;        /* disable wakeup timer */
    while (!(RTC->ISR & RTC_ISR_WUTWF));
    RTC->WUTR = seconds * 2 - 1;    /* 2Hz clock → seconds*2 ticks */
    RTC->CR |= RTC_CR_WUTE | RTC_CR_WUTIE;

    EXTI->IMR  |= EXTI_IMR_MR22;    /* RTC wakeup EXTI line */
    EXTI->RTSR |= EXTI_RTSR_TR22;   /* rising edge */
    NVIC_SetPriority(RTC_WKUP_IRQn, 0);
    NVIC_EnableIRQ(RTC_WKUP_IRQn);
}

void RTC_WKUP_IRQHandler(void) {
    RTC->ISR &= ~RTC_ISR_WUTF;      /* clear flag */
    EXTI->PR = EXTI_PR_PR22;
}
```

## GPIO State for Low Power

Floating inputs consume ~100µA each. Before sleep:

```c
void gpio_lowpower_config(void) {
    /* Set all unused pins to analog mode (lowest power) */
    GPIOA->MODER = 0xFFFFFFFF;  /* all analog */
    GPIOB->MODER = 0xFFFFFFFF;
    GPIOC->MODER = 0xFFFFFFFF;
    /* ... etc */

    /* Keep only the pins you need (e.g., UART TX/RX, wake pin) */
    GPIOA->MODER = (2U << (2 * 2)) | (2U << (3 * 2));  /* PA2/PA3 as AF for UART */
}
```

## Peripheral Clock Gating

Disable clocks to unused peripherals in `RCC->XXENR`. The MCU consumes power for any clocked peripheral, even if not used.

```c
void disable_unused_peripherals(void) {
    RCC->AHB1ENR &= ~(RCC_AHB1ENR_CRCEN | RCC_AHB1ENR_DMA1EN);
    RCC->APB1ENR &= ~(RCC_APB1ENR_TIM2EN | RCC_APB1ENR_TIM3EN | RCC_APB1ENR_TIM4EN);
    RCC->APB2ENR &= ~(RCC_APB2ENR_TIM8EN | RCC_APB2ENR_ADC2EN | RCC_APB2ENR_ADC3EN);
}
```

## Low-Power UART (LPUART)

STM32L and newer STM32U/H series have an LPUART that can run in Stop mode and wake the MCU on data:

```c
/* LPUART1 on PC10/PC11, runs on LSE (32.768kHz) in Stop mode */
RCC->APB1ENR2 |= RCC_APB1ENR2_LPUART1EN;
LPUART1->BRR = 0x36B85;  /* 32.768kHz → 9600 baud */
LPUART1->CR1 = USART_CR1_UE | USART_CR1_TE | USART_CR1_RE | USART_CR1_RXNEIE;
LPUART1->CR3 = USART_CR3_UCFT | USART_CR3_WUS_1;  /* wake on start bit */
```

## Low-Power Timers (LPTIM)

LPTIM runs on LSE/LSE in Stop mode. Use for periodic wake-ups without RTC:

```c
RCC->APB1ENR1 |= RCC_APB1ENR1_LPTIM1EN;
LPTIM1->CFGR = LPTIM_CFGR_PRESC_0;  /* /2 */
LPTIM1->CR = LPTIM_CR_ENABLE;
LPTIM1->ARR = 32768;  /* 1 second with 32.768kHz LSE */
LPTIM1->DIER = LPTIM_DIER_ARRMIE;
LPTIM1->CR |= LPTIM_CR_CNTSTRT;
```

## ESP32 Low-Power Modes

ESP32 has different power management than STM32:

| Mode | Current | Wake source | Notes |
|---|---|---|---|
| Active | 80–240 mA | — | Wi-Fi/BLE active |
| Modem sleep | 20 mA | Wi-Fi beacon | Auto when Wi-Fi idle |
| Light sleep | 0.8 mA | GPIO, timer, UART | RAM retained |
| Deep sleep | 10–150 µA | RTC GPIO, timer, touch | RAM lost; only RTC survives |
| Hibernation | 5 µA | RTC timer only | Minimal state |

```c
#include "esp_sleep.h"

void enter_deep_sleep(uint32_t seconds) {
    esp_sleep_enable_timer_wakeup(seconds * 1000000ULL);  /* microseconds */
    /* Optional: wake on GPIO (RTC GPIO only) */
    esp_sleep_enable_ext0_wakeup(GPIO_NUM_0, 0);  /* wake on LOW */
    esp_deep_sleep_start();  /* never returns; resets on wake */
}
```

## Battery Selection

| Chemistry | Voltage | Capacity | Self-discharge | Best for |
|---|---|---|---|---|
| CR2032 (coin) | 3V | 220 mAh | 1%/year | Tiny devices, 1+ year life |
| AA alkaline | 1.5V | 2500 mAh | 3%/year | Cheap, available |
| Li-ion (18650) | 3.7V | 3000 mAh | 2%/month | High-drain, rechargeable |
| LiFeS2 (AA) | 1.5V | 3000 mAh | 0.6%/year | High-drain primary |
| Li-SOCl2 | 3.6V | 19000 mAh (D) | 1%/year | 10+ year life, low current |

## Estimating Battery Life

```python
# Example: STM32L0 in Stop mode with 1s sensor reads
sleep_current_uA = 1.0       # Stop mode
active_current_mA = 8.0      # Run mode
active_time_ms = 50          # ms per cycle
sleep_time_ms = 950          # ms per cycle

duty_cycle = active_time_ms / (active_time_ms + sleep_time_ms)
avg_current_uA = (active_current_mA * 1000 * duty_cycle) + \
                 (sleep_current_uA * (1 - duty_cycle))
# = (8000 * 0.05) + (1 * 0.95) = 400 + 0.95 = 400.95 µA

battery_mAh = 220  # CR2032
life_hours = battery_mAh * 1000 / avg_current_uA
life_days = life_hours / 24
print(f"Estimated life: {life_days:.1f} days")
# Output: ~22.9 days
```

## Reducing Active Time

The biggest lever is reducing time in Run mode, not reducing sleep current:

1. **DMA everything** — don't busy-wait on UART/ADC
2. **Hardware crypto** — don't compute AES in software
3. **Wake → measure → sleep fast** — minimize time between wake and re-sleep
4. **Batch transmissions** — send 100 readings once per hour, not 1 reading every 36 seconds
5. **Lower clock speed** — if 48MHz works, don't run at 168MHz
6. **Disable Wi-Fi/BLE when not needed** — radio is the biggest power draw

## Common Pitfalls

| Pitfall | Impact | Fix |
|---|---|---|
| Floating GPIO inputs | +100µA per pin | Set unused pins to analog |
| Brown-out detector disabled | Random resets | Keep BOR enabled |
| Watchdog too aggressive | Wake-ups during sleep | Disable WDT in sleep or use IWDG with long timeout |
| Wi-Fi always on | 100mA draw | Use modem sleep; disconnect between transmissions |
| ADC left enabled | +1mA | Disable ADC clock before sleep |
| Wrong voltage regulator mode | Higher quiescent current | Use low-power regulator mode in Stop |
| Wake source not configured | MCU never wakes | Test wake source in Run mode first |
| Long crystal startup | +5ms active per wake | Use HSI (faster start) for short wakes |
| Measurement not real | Estimates wrong | Use a current analyzer (Otii, Joulescope) |
