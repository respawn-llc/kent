fn main() {
    if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("macos") {
        cc::Build::new()
            .file("src/speech_macos.m")
            .flag("-fobjc-arc")
            .flag("-fblocks")
            .compile("kent_speech_macos");
        for framework in [
            "Foundation",
            "AVFoundation",
            "CoreMedia",
            "UniformTypeIdentifiers",
        ] {
            println!("cargo:rustc-link-lib=framework={framework}");
        }
        println!("cargo:rerun-if-changed=src/speech_macos.h");
        println!("cargo:rerun-if-changed=src/speech_macos.m");
    }
    tauri_build::build()
}
