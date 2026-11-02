//! Codec throughput probe (dev tool, not part of the SDK).
//!
//! Run with: `cargo run --release --example bench`
//!
//! For context: the PXB codec costs ~0.1–0.2 µs per message. Real extension
//! latency is dominated by process spawn + pipe round trips, not this.

use std::time::Instant;

use phi_ext::pxb;

fn bench(name: &str, iters: usize, mut f: impl FnMut()) {
    // Warm up.
    for _ in 0..iters / 10 {
        f();
    }
    let t0 = Instant::now();
    for _ in 0..iters {
        f();
    }
    let ns_op = t0.elapsed().as_nanos() as f64 / iters as f64;
    println!("{name:<34} {ns_op:8.1} ns/op   {:>9.1} Mops/s", 1e3 / ns_op);
}

fn main() {
    let iters = 2_000_000usize;

    bench("encode+decode Hello (small)", iters, || {
        let b = pxb::Hello {
            name: "greet".into(),
            version: "1.0.0".into(),
            caps: 3,
            protocol: 1,
        }
        .encode();
        std::hint::black_box(pxb::Hello::decode(&b).unwrap());
    });

    let req = pxb::InterceptReq {
        event: pxb::Event::ToolCall.code(),
        tool_name: "bash".into(),
        tool_call_id: "call_01JX9K2M".into(),
        input: br#"{"command":"ls -la /tmp","cwd":"/workspace"}"#.to_vec(),
        turn_index: 3,
        ..Default::default()
    };
    let frame = req.encode();
    println!("\nInterceptReq payload: {} bytes", frame.len());
    bench("encode+decode InterceptReq (tool_call)", iters, || {
        let b = req.encode();
        let d = pxb::InterceptReq::decode(&b).unwrap();
        std::hint::black_box(d.tool_call_id);
    });

    let ev = pxb::EventNotify {
        event: pxb::Event::ToolResult.code(),
        tool_name: "bash".into(),
        tool_call_id: "call_01JX9K2M".into(),
        input: req.input.clone(),
        turn_index: 3,
        session_id: "sess_01JX".into(),
        ..Default::default()
    };
    let frame = ev.encode();
    println!("EventNotify payload: {} bytes", frame.len());
    bench("encode+decode EventNotify (tool_result)", iters, || {
        let b = ev.encode();
        let d = pxb::EventNotify::decode(&b).unwrap();
        std::hint::black_box(d.session_id);
    });

    // In-memory frame read/write: the syscall-free floor for one RPC.
    bench("write_frame+read_frame (in-memory)", iters / 4, || {
        let mut buf = Vec::new();
        pxb::write_frame(&mut buf, pxb::TYPE_INTERCEPT, pxb::FLAG_HAS_ID, 42, &frame).unwrap();
        let mut cur = std::io::Cursor::new(buf);
        let f = pxb::read_frame(&mut cur).unwrap();
        std::hint::black_box(f.body.len());
    });
}
