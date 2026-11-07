use std::path::PathBuf;

fn main() {
    link_mobile_core();
    tauri_build::build()
}

/// Android/iOS：链接移动端 Go 内核（由 scripts/build-mobile-core.sh 生成到 mobile-libs/<target>/）。
/// 桌面端不需要（内核以 sidecar 运行）。
fn link_mobile_core() {
    let os = std::env::var("CARGO_CFG_TARGET_OS").unwrap_or_default();
    if os != "android" && os != "ios" {
        return;
    }
    let target = std::env::var("TARGET").unwrap();
    let dir = std::env::var("TEAMSYNC_MOBILE_LIB_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|_| PathBuf::from(std::env::var("CARGO_MANIFEST_DIR").unwrap()).join("mobile-libs").join(&target));
    let file = if os == "android" { "libteamsynccore.so" } else { "libteamsynccore.a" };
    let lib = dir.join(file);
    println!("cargo:rerun-if-changed={}", lib.display());
    println!("cargo:rerun-if-env-changed=TEAMSYNC_MOBILE_LIB_DIR");
    if !lib.exists() {
        panic!(
            "找不到移动端内核 {}，请先运行 scripts/build-mobile-core.sh {} {}",
            lib.display(),
            if os == "android" { "android" } else { "ios" },
            target
        );
    }
    println!("cargo:rustc-link-search=native={}", dir.display());
    if os == "android" {
        println!("cargo:rustc-link-lib=dylib=teamsynccore");
    } else {
        println!("cargo:rustc-link-lib=static=teamsynccore");
        for fw in ["CoreFoundation", "Security", "SystemConfiguration"] {
            println!("cargo:rustc-link-lib=framework={fw}");
        }
        println!("cargo:rustc-link-lib=resolv");
    }
}
