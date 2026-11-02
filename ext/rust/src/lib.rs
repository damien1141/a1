//! Rust author SDK for [a1] PXB extensions — a faithful port of the Go SDK
//! under `ext/go`.
//!
//! Two layers, mirroring the Go module:
//!
//! - [`pxb`]: the binary wire protocol (frames, tagged fields, message codecs)
//! - [`phi`]: the author-facing API ([`phi::Extension`]) that speaks PXB over
//!   stdin/stdout
//!
//! The only external dependencies are `serde`/`serde_json` at the JSON edges
//! (tool schemas, confirm payloads) plus `tokio` (`rt` feature) to drive async
//! tool handlers; the wire format itself needs no reflection, so the PXB
//! codecs stay hand-rolled and lean.
//!
//! # Example
//!
//! ```no_run
//! use phi_ext::{phi, pxb};
//!
//! fn main() -> Result<(), phi::Error> {
//!     let mut m = phi::Extension::new("hello", "0.1.0");
//!     m.register_command(
//!         "hello",
//!         phi::Command::new("Say hi", |_args, ctx| {
//!             ctx.notify("info", "Hello!");
//!             Ok(())
//!         }),
//!     );
//!     m.subscribe(pxb::Event::SessionStart, |_ev| {});
//!     m.run()
//! }
//! ```
//!
//! [a1]: https://github.com/damien1141/a1

pub mod phi;
pub mod pxb;
