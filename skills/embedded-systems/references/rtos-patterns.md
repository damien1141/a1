# RTOS Patterns Reference

FreeRTOS and Zephyr patterns for concurrent firmware. Covers task design, queues, semaphores/mutexes, scheduling, priority inversion, and deadline management. Read alongside `microcontroller-programming.md` for ISRs and peripherals.

## When to Use an RTOS

| Use bare-metal | Use RTOS |
|---|---|
| One or two activities, no hard deadlines | Multiple concurrent activities |
| Tight memory (<8KB flash) | Enough flash for kernel (~10KB+) |
| Simple state machine | Complex state across many components |
| Real-time but sequential | Multiple real-time deadlines |
| Always-on, low-power | Need blocking I/O without busy-waiting |
| Strict safety certification, simple is safer | Need TCP/IP, BLE, filesystem |

If you're not sure, start bare-metal. Adding an RTOS later is easier than removing one.

## FreeRTOS

### Task Creation

```c
#include "FreeRTOS.h"
#include "task.h"
#include "queue.h"
#include "semphr.h"

/* Priorities: 0 = idle, configMAX_PRIORITIES-1 = highest */
#define PRIO_SENSOR     (tskIDLE_PRIORITY + 3)
#define PRIO_PROCESS    (tskIDLE_PRIORITY + 2)
#define PRIO_COMM       (tskIDLE_PRIORITY + 4)
#define PRIO_LOGGER     (tskIDLE_PRIORITY + 1)  /* low — non-critical */

/* Stack sizes are in WORDS (4 bytes each on 32-bit), not bytes */
#define STACK_SENSOR    256   /* 1KB */
#define STACK_PROCESS   512   /* 2KB */
#define STACK_LOGGER    384   /* 1.5KB */

static TaskHandle_t xSensorTaskHandle, xProcessTaskHandle;

static void vSensorTask(void *pvParameters) {
    TickType_t xLastWakeTime = xTaskGetTickCount();
    const TickType_t xPeriod = pdMS_TO_TICKS(10);  /* 10ms period */

    for (;;) {
        uint16_t raw = adc_read(ADC_CH0);
        xQueueSend(xSensorQueue, &raw, 0);

        /* Stack high-water check in debug builds */
        configASSERT(uxTaskGetStackHighWaterMark(NULL) > 32);

        /* Periodic deadline — vTaskDelayUntil is more precise than vTaskDelay */
        vTaskDelayUntil(&xLastWakeTime, xPeriod);
    }
}

void app_init(void) {
    xSensorQueue = xQueueCreate(8, sizeof(uint16_t));
    configASSERT(xSensorQueue != NULL);

    xTaskCreate(vSensorTask,    "Sensor",  STACK_SENSOR,  NULL, PRIO_SENSOR,  &xSensorTaskHandle);
    xTaskCreate(vProcessTask,   "Process", STACK_PROCESS, NULL, PRIO_PROCESS, &xProcessTaskHandle);
    xTaskCreate(vCommTask,      "Comm",    256,           NULL, PRIO_COMM,    NULL);
    xTaskCreate(vLoggerTask,    "Logger",  STACK_LOGGER,  NULL, PRIO_LOGGER,  NULL);

    vTaskStartScheduler();  /* never returns */
}
```

### Queues (task-to-task communication)

```c
typedef struct {
    uint16_t sensor_a;
    uint16_t sensor_b;
    uint32_t timestamp;
} SensorPacket_t;

QueueHandle_t xSensorQueue;

void InitQueues(void) {
    xSensorQueue = xQueueCreate(16, sizeof(SensorPacket_t));
    configASSERT(xSensorQueue != NULL);
}

/* Producer (ISR-safe) */
void EXTI0_IRQHandler(void) {
    BaseType_t xHigherPriorityTaskWoken = pdFALSE;
    SensorPacket_t pkt = { .sensor_a = adc_read(0), .sensor_b = adc_read(1),
                           .timestamp = xTaskGetTickCountFromISR() };
    xQueueSendFromISR(xSensorQueue, &pkt, &xHigherPriorityTaskWoken);
    EXTI->PR = EXTI_PR_PR0;  /* clear interrupt flag */
    portYIELD_FROM_ISR(xHigherPriorityTaskWoken);
}

/* Consumer (task) */
void vProcessTask(void *pv) {
    SensorPacket_t pkt;
    for (;;) {
        if (xQueueReceive(xSensorQueue, &pkt, portMAX_DELAY) == pdPASS) {
            process(&pkt);
        }
    }
}
```

### Semaphores & Mutexes

```c
/* Binary semaphore — signaling (ISR → task) */
SemaphoreHandle_t xUartSemaphore;

void USART2_IRQHandler(void) {
    BaseType_t xHigherPriorityTaskWoken = pdFALSE;
    xSemaphoreGiveFromISR(xUartSemaphore, &xHigherPriorityTaskWoken);
    portYIELD_FROM_ISR(xHigherPriorityTaskWoken);
}

void vUartTask(void *pv) {
    for (;;) {
        xSemaphoreTake(xUartSemaphore, portMAX_DELAY);
        /* process UART data */
    }
}

/* Counting semaphore — resource pool (e.g., 5 DMA channels) */
SemaphoreHandle_t xDmaPool = xSemaphoreCreateCounting(5, 5);

/* Mutex — protecting shared resource (has priority inheritance!) */
SemaphoreHandle_t xI2cMutex = xSemaphoreCreateMutex();

void safe_i2c_write(uint8_t addr, uint8_t *data, uint16_t len) {
    xSemaphoreTake(xI2cMutex, pdMS_TO_TICKS(100));
    I2C_Write(addr, data, len);   /* exclusive access */
    xSemaphoreGive(xI2cMutex);
}
```

**Mutex vs Binary Semaphore:** Mutex has priority inheritance (high-prio task waiting on a low-prio task boosts the low-prio task). Always use mutex for resource protection; use binary semaphore for pure signaling.

### Priority Inversion

Problem: High-prio task H waits on mutex held by low-prio task L. Medium-prio task M preempts L. H is now blocked by M (lower priority) — inversion.

Fix: FreeRTOS mutexes use **priority inheritance** — when H blocks on L's mutex, L temporarily inherits H's priority until it releases. M can't preempt L.

This is why you must use `xSemaphoreCreateMutex()` (with inheritance) not `xSemaphoreCreateBinary()` for resource protection.

### Task Notifications (lightweight signaling)

For one-to-one task signaling, task notifications are 50% faster and use less RAM than semaphores:

```c
#define RX_READY_BIT  (1 << 0)

void USART2_IRQHandler(void) {
    BaseType_t xHigherPriorityTaskWoken = pdFALSE;
    xTaskNotifyFromISR(xUartTaskHandle, RX_READY_BIT, eSetBits,
                       &xHigherPriorityTaskWoken);
    portYIELD_FROM_ISR(xHigherPriorityTaskWoken);
}

void vUartTask(void *pv) {
    uint32_t notify_value;
    for (;;) {
        xTaskNotifyWait(0, RX_READY_BIT, &notify_value, portMAX_DELAY);
        if (notify_value & RX_READY_BIT) {
            /* process UART */
        }
    }
}
```

### Software Timers

```c
void vHeartbeatTimer(TimerHandle_t xTimer) {
    gpio_toggle(LED_HEARTBEAT);
}

TimerHandle_t xHeartbeat = xTimerCreate("Heartbeat", pdMS_TO_TICKS(1000),
                                        pdTRUE,  /* auto-reload */
                                        NULL, vHeartbeatTimer);
xTimerStart(xHeartbeat, 0);
```

Don't do heavy work in timer callbacks — they run in the timer service task. Send to a queue and let another task process.

## Zephyr

Zephyr is a Linux Foundation RTOS with device tree, 200+ board configs, and a rich driver ecosystem. Preferred for new projects needing BLE, TCP/IP, or multi-MCU portability.

### Threads (Zephyr equivalent of tasks)

```c
#include <zephyr/kernel.h>

#define MY_STACK_SIZE 1024
#define MY_PRIORITY 5

void my_thread(void *a, void *b, void *c) {
    while (1) {
        /* ... */
        k_sleep(K_MSEC(100));
    }
}

K_THREAD_DEFINE(my_tid, MY_STACK_SIZE, my_thread, NULL, NULL, NULL,
                MY_PRIORITY, 0, 0);
```

### Kernel Objects

```c
/* Semaphore */
K_SEM_DEFINE(my_sem, 0, 1);              /* initial 0, max 1 */
k_sem_take(&my_sem, K_FOREVER);
k_sem_give(&my_sem);

/* Mutex */
K_MUTEX_DEFINE(my_mutex);
k_mutex_lock(&my_mutex, K_FOREVER);
/* ... */
k_mutex_unlock(&my_mutex);

/* Queue */
K_QUEUE_DEFINE(my_queue);
k_queue_put(&my_queue, (void *)&my_data);
void *received = k_queue_get(&my_queue, K_FOREVER);

/* FIFO (typed) */
struct my_item { void *fifo_reserved; uint32_t value; };
K_FIFO_DEFINE(my_fifo);
struct my_item *item = k_fifo_get(&my_fifo, K_FOREVER);
```

### Device Tree (Zephyr's portability layer)

```dts
/* app.overlay — pin/peripheral config per board */
&uart2 {
    status = "okay";
    current-speed = <115200>;
    pinctrl-0 = <&uart2_default>;
    pinctrl-names = "default";
};
```

```c
/* Access device from C */
const struct device *uart = DEVICE_DT_GET(DT_NODELABEL(uart2));
if (!device_is_ready(uart)) {
    printk("UART2 not ready\n");
    return;
}
```

Device tree lets the same C code run on different MCUs by changing only the overlay file.

## Scheduling Patterns

### Rate-Monotonic Scheduling

For periodic tasks: assign priorities so shorter-period tasks have higher priority. This is **rate-mononotonic scheduling** and it's the optimal static-priority scheme.

```
Task A: 10ms period  → highest priority
Task B: 50ms period  → medium priority
Task C: 100ms period → lowest priority
```

**Utilization bound:** RM is schedulable if total utilization ≤ n·(2^(1/n) - 1), where n = number of tasks. For n=3, that's ~78%. Leave headroom.

### Deadline-Monotonic

For tasks with deadlines < period: assign by deadline, not period. Shorter deadline = higher priority.

### Cooperative vs Preemptive

- **Preemptive** (default in FreeRTOS): kernel can interrupt a task for a higher-prio one. Real-time, but more sync bugs.
- **Cooperative**: tasks yield voluntarily. Simpler, but hard to guarantee deadlines.

Use preemptive for real-time; cooperative only for simple systems.

## Deadline Management

```c
/* Assert deadline met — abort or degrade if not */
void vControlTask(void *pv) {
    TickType_t xLastWakeTime = xTaskGetTickCount();
    const TickType_t xPeriod = pdMS_TO_TICKS(5);  /* 5ms deadline */

    for (;;) {
        compute_control_loop();

        TickType_t xElapsed = xTaskGetTickCount() - xLastWakeTime;
        if (xElapsed > xPeriod) {
            /* DEADLINE MISS — log and degrade */
            log_deadline_miss(xElapsed);
            enter_safe_mode();
        }

        vTaskDelayUntil(&xLastWakeTime, xPeriod);
    }
}
```

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Stack too small | HardFault / corruption | Measure high-water; add 20% headroom |
| Priority inversion | High-prio task stalls | Use mutex (priority inheritance) |
| ISR too long | Watchdog reset / missed ISRs | Defer to task via queue/notification |
| ISR calls non-FromISR API | Kernel panic | Use only `*FromISR` variants in ISRs |
| Task starves low-prio task | Other tasks slow | Add idle hook; lower high-prio rate |
| Queue full silently | Lost data | Check return; use larger queue or backpressure |
| `vTaskDelay` instead of `vTaskDelayUntil` | Drift over time | Use `vTaskDelayUntil` for periodic |
| Mutex from ISR | Crash | ISRs can't take mutexes (only binary sem) |
| Shared resource without lock | Corruption | Always lock shared resources |
| `printf` in ISR | Stall | Use ITM/SWO or queue-and-print from task |
