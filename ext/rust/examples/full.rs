//! Richer example: a tool, a confirm dialog, and a queued follow-up.

use phi_ext::phi;

fn main() -> Result<(), phi::Error> {
    let mut m = phi::Extension::new("full", "0.1.0");

    m.register_tool(
        phi::Tool::new(
            "echo",
            "Echo the input back",
            phi::Schema::object()
                .property("text", phi::Schema::string())
                .required(["text"]),
            |args| {
                let text = String::from_utf8_lossy(args);
                Ok(phi::ToolResult {
                    content: format!("echo: {text}"),
                    ..Default::default()
                })
            },
        )
        .detail_from_args(|args| String::from_utf8_lossy(args).into_owned()),
    );

    m.register_tool(
        phi::Tool::new_async(
            "async-echo",
            "Echo the input back (async handler)",
            phi::Schema::object()
                .property("text", phi::Schema::string())
                .required(["text"]),
            |args| async move {
                // Yield once: proves the SDK's runtime drives the future
                // rather than just polling a ready block.
                tokio::task::yield_now().await;
                let text = String::from_utf8_lossy(&args);
                Ok(phi::ToolResult {
                    content: format!("async echo: {text}"),
                    ..Default::default()
                })
            },
        )
        .timeout_sec(10),
    );

    m.register_command(
        "ask",
        phi::Command::new("Ask a yes/no question", |_args, ctx| {
            let reply = ctx.confirm("Confirm?", "Proceed with /tmp/x?");
            if reply.ok {
                ctx.notify("info", "Confirmed!");
            } else {
                ctx.notify("warning", "Declined.");
            }
            ctx.submit("follow-up from ask");
            Ok(())
        }),
    );

    m.run()
}
