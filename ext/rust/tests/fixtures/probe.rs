use phi_ext::{phi, pxb};
use std::time::Duration;

fn main() -> Result<(), phi::Error> {
    let mut ext = phi::Extension::new("sdk-probe", "0.1.0");
    ext.register_tool(phi::Tool::new_async(
        "io",
        "Exercise runtime drivers",
        phi::Schema::object(),
        |args| async move {
            tokio::time::sleep(Duration::from_millis(10)).await;
            let address = String::from_utf8(args).map_err(|e| e.to_string())?;
            let stream = tokio::net::TcpStream::connect(address)
                .await
                .map_err(|e| e.to_string())?;
            let mut byte = [0];
            loop {
                stream.readable().await.map_err(|e| e.to_string())?;
                match stream.try_read(&mut byte) {
                    Ok(1) => break,
                    Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => continue,
                    other => return Err(format!("expected loopback byte: {other:?}")),
                }
            }
            Ok(phi::ToolResult {
                content: byte[0].to_string(),
                ..Default::default()
            })
        },
    ));
    ext.register_tool(phi::Tool::new(
        "large",
        "Large result",
        phi::Schema::object(),
        |_| {
            Ok(phi::ToolResult {
                content: "x".repeat(pxb::MAX_PAYLOAD),
                ..Default::default()
            })
        },
    ));
    ext.register_command(
        "ask",
        phi::Command::new("Confirm", |_, ctx| {
            ctx.confirm("Proceed?", "Test");
            // A second confirmation must not read again after shutdown.
            ctx.confirm("Again?", "Test");
            Ok(())
        }),
    );
    ext.run()
}
