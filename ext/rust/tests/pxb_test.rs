//! Byte-level compatibility tests against the Go SDK's golden fixtures
//! (`ext/go/pxb/testdata/*.bin`, regenerated with `UPDATE_GOLDEN=1 go test`).
//! Skipped when the repo layout is absent (e.g. crate published standalone).

use std::io::Cursor;
use std::path::PathBuf;

use phi_ext::pxb;

fn testdata(name: &str) -> Option<Vec<u8>> {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../go/pxb/testdata")
        .join(name);
    std::fs::read(path).ok()
}

/// A decode-then-re-encode pass must reproduce the Go bytes exactly.
#[test]
fn golden_hello() {
    let Some(raw) = testdata("hello.bin") else {
        return;
    };
    let h = pxb::Hello::decode(&raw).unwrap();
    assert_eq!(h.name, "greet");
    assert_eq!(h.version, "1.0.0");
    assert_eq!(h.caps, pxb::CAP_TOOLS | pxb::CAP_COMMANDS);
    assert_eq!(h.protocol, 1);
    assert_eq!(h.encode(), raw);
}

#[test]
fn golden_hello_ack() {
    let Some(raw) = testdata("hello_ack.bin") else {
        return;
    };
    let h = pxb::HelloAck::decode(&raw).unwrap();
    assert_eq!(h.protocol, 1);
    assert_eq!(h.phi_version, "v0.19.0");
    assert_eq!(h.cwd, "/tmp");
    assert_eq!(h.session_id, "s1");
    assert_eq!(h.extension_dir, "/ext");
    assert_eq!(h.encode(), raw);
}

#[test]
fn golden_subscribe() {
    let Some(raw) = testdata("subscribe.bin") else {
        return;
    };
    let s = pxb::Subscribe::decode(&raw).unwrap();
    assert_eq!(
        s.events,
        vec![pxb::Event::SessionStart.code(), pxb::Event::AgentEnd.code()]
    );
    assert_eq!(s.intercept, vec![pxb::Event::ToolCall.code()]);
    assert_eq!(s.encode(), raw);
}

#[test]
fn golden_intercept_req() {
    let Some(raw) = testdata("intercept_req.bin") else {
        return;
    };
    let r = pxb::InterceptReq::decode(&raw).unwrap();
    assert_eq!(r.event, pxb::Event::ToolCall.code());
    assert_eq!(r.tool_name, "bash");
    assert_eq!(r.tool_call_id, "c1");
    assert_eq!(r.input, br#"{"command":"ls"}"#);
    assert!(!r.is_error);
    assert_eq!(r.encode(), raw);
}

/// The framed fixture is a full `TypeHello` frame; the payload must equal
/// `hello.bin` and the header must parse.
#[test]
fn golden_hello_frame() {
    let Some(raw) = testdata("hello_frame.bin") else {
        return;
    };
    let f = pxb::read_frame(&mut Cursor::new(raw)).unwrap();
    assert_eq!(f.header.typ, pxb::TYPE_HELLO);
    assert_eq!(f.header.flags, 0);
    assert_eq!(f.header.id, 0);
    assert_eq!(f.body, testdata("hello.bin").unwrap());
}
