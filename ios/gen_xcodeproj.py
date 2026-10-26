#!/usr/bin/env python3
"""Generate PhotoSync.xcodeproj without XcodeGen.

The build machine (a headless Mac) has no network access, so `brew install
xcodegen` is not an option. This script emits a minimal but valid pbxproj that
Xcode 15 / xcodebuild can consume directly.

It is intentionally dependency-free: only the Python standard library.

Usage:
    python3 gen_xcodeproj.py [--output PhotoSync.xcodeproj]
"""

from __future__ import annotations

import argparse
import hashlib
import os
import shutil
import sys

# --------------------------------------------------------------------------- #
# Swift sources: discovered from disk so adding a file needs no changes here.
# --------------------------------------------------------------------------- #
SOURCE_ROOT = "PhotoSync"

# Groups are derived from the directory layout (Core/, Server/, UI/).
GROUP_ORDER = ["Core", "Server", "UI"]


def stable_id(*parts: str) -> str:
    """A deterministic 24-hex-char object id.

    Xcode only requires uniqueness and the 24-hex format; deriving it from the
    path keeps regenerated projects byte-identical, which makes diffs readable.
    """
    digest = hashlib.sha256("|".join(parts).encode()).hexdigest()
    return digest[:24].upper()


def discover_sources() -> dict[str, list[str]]:
    """Maps group name -> sorted list of file names inside that group."""
    groups: dict[str, list[str]] = {}
    for group in GROUP_ORDER:
        path = os.path.join(SOURCE_ROOT, group)
        if not os.path.isdir(path):
            continue
        files = sorted(f for f in os.listdir(path) if f.endswith(".swift"))
        if files:
            groups[group] = files
    return groups


# --------------------------------------------------------------------------- #
# pbxproj writer
# --------------------------------------------------------------------------- #
class Project:
    def __init__(self, name: str):
        self.name = name
        self.objects: list[str] = []

    def add(self, body: str) -> str:
        self.objects.append(body)
        return body


def build(groups: dict[str, list[str]], deployment: str, version: str,
          build_number: str) -> str:
    # ---- ids -------------------------------------------------------------
    id_project = stable_id("project")
    id_target = stable_id("target")
    id_product = stable_id("product")
    id_main_group = stable_id("group", "main")
    id_products_group = stable_id("group", "Products")
    id_app_group = stable_id("group", "PhotoSync")
    id_sources_phase = stable_id("phase", "sources")
    id_frameworks_phase = stable_id("phase", "frameworks")
    id_resources_phase = stable_id("phase", "resources")
    id_build_config_list_project = stable_id("configlist", "project")
    id_build_config_list_target = stable_id("configlist", "target")
    id_config_project_debug = stable_id("config", "project", "Debug")
    id_config_project_release = stable_id("config", "project", "Release")
    id_config_target_debug = stable_id("config", "target", "Debug")
    id_config_target_release = stable_id("config", "target", "Release")

    # ---- file references & build files ----------------------------------
    file_refs: list[tuple[str, str, str, str]] = []   # (id, name, group, path)
    group_children: dict[str, list[str]] = {}
    build_files: list[tuple[str, str]] = []           # (buildfile id, fileref id)

    for group, files in groups.items():
        group_children[group] = []
        for name in files:
            ref = stable_id("fileref", group, name)
            bf = stable_id("buildfile", group, name)
            file_refs.append((ref, name, group, name))
            group_children[group].append(ref)
            build_files.append((bf, ref))

    # ---- sections --------------------------------------------------------
    out: list[str] = []
    w = out.append

    w("// !$*UTF8*$!")
    w("{")
    w("\tarchiveVersion = 1;")
    w("\tclasses = {")
    w("\t};")
    w("\tobjectVersion = 56;")
    w("\tobjects = {")

    # PBXBuildFile
    w("\n/* Begin PBXBuildFile section */")
    for bf, ref in build_files:
        w(f"\t\t{bf} /* file */ = {{isa = PBXBuildFile; fileRef = {ref} /* file */; }};")
    w("/* End PBXBuildFile section */")

    # PBXFileReference
    w("\n/* Begin PBXFileReference section */")
    w(f'\t\t{id_product} /* {PRODUCT_NAME}.app */ = {{isa = PBXFileReference; '
      f'explicitFileType = wrapper.application; includeInIndex = 0; '
      f'path = {PRODUCT_NAME}.app; sourceTree = BUILT_PRODUCTS_DIR; }};')
    for ref, name, _group, path in file_refs:
        w(f'\t\t{ref} /* {name} */ = {{isa = PBXFileReference; '
          f'lastKnownFileType = sourcecode.swift; path = "{path}"; '
          f'sourceTree = "<group>"; }};')
    w("/* End PBXFileReference section */")

    # PBXFrameworksBuildPhase
    w("\n/* Begin PBXFrameworksBuildPhase section */")
    w(f"\t\t{id_frameworks_phase} /* Frameworks */ = {{")
    w("\t\t\tisa = PBXFrameworksBuildPhase;")
    w("\t\t\tbuildActionMask = 2147483647;")
    w("\t\t\tfiles = (")
    w("\t\t\t);")
    w("\t\t\trunOnlyForDeploymentPostprocessing = 0;")
    w("\t\t};")
    w("/* End PBXFrameworksBuildPhase section */")

    # PBXGroup (main)
    w("\n/* Begin PBXGroup section */")
    w(f"\t\t{id_main_group} = {{")
    w("\t\t\tisa = PBXGroup;")
    w("\t\t\tchildren = (")
    w(f"\t\t\t\t{id_app_group} /* PhotoSync */,")
    w(f"\t\t\t\t{id_products_group} /* Products */,")
    w("\t\t\t);")
    w('\t\t\tsourceTree = "<group>";')
    w("\t\t};")

    # app group children = subgroup groups
    w(f"\t\t{id_app_group} /* PhotoSync */ = {{")
    w("\t\t\tisa = PBXGroup;")
    w("\t\t\tchildren = (")
    for group in groups:
        w(f"\t\t\t\t{stable_id('group', group)} /* {group} */,")
    w("\t\t\t);")
    w(f'\t\t\tpath = {SOURCE_ROOT};')
    w('\t\t\tsourceTree = "<group>";')
    w("\t\t};")

    # one group per subdirectory
    for group, children in group_children.items():
        w(f"\t\t{stable_id('group', group)} /* {group} */ = {{")
        w("\t\t\tisa = PBXGroup;")
        w("\t\t\tchildren = (")
        for child in children:
            w(f"\t\t\t\t{child},")
        w("\t\t\t);")
        w(f'\t\t\tpath = {group};')
        w('\t\t\tsourceTree = "<group>";')
        w("\t\t};")

    # products
    w(f"\t\t{id_products_group} /* Products */ = {{")
    w("\t\t\tisa = PBXGroup;")
    w("\t\t\tchildren = (")
    w(f"\t\t\t\t{id_product} /* {PRODUCT_NAME}.app */,")
    w("\t\t\t);")
    w('\t\t\tname = Products;')
    w('\t\t\tsourceTree = "<group>";')
    w("\t\t};")
    w("/* End PBXGroup section */")

    # PBXNativeTarget
    w("\n/* Begin PBXNativeTarget section */")
    w(f"\t\t{id_target} /* {PRODUCT_NAME} */ = {{")
    w("\t\t\tisa = PBXNativeTarget;")
    w(f"\t\t\tbuildConfigurationList = {id_build_config_list_target};")
    w("\t\t\tbuildPhases = (")
    w(f"\t\t\t\t{id_sources_phase} /* Sources */,")
    w(f"\t\t\t\t{id_frameworks_phase} /* Frameworks */,")
    w(f"\t\t\t\t{id_resources_phase} /* Resources */,")
    w("\t\t\t);")
    w("\t\t\tbuildRules = (")
    w("\t\t\t);")
    w("\t\t\tdependencies = (")
    w("\t\t\t);")
    w(f"\t\t\tname = {PRODUCT_NAME};")
    w(f"\t\t\tproductName = {PRODUCT_NAME};")
    w(f"\t\t\tproductReference = {id_product} /* {PRODUCT_NAME}.app */;")
    w('\t\t\tproductType = "com.apple.product-type.application";')
    w("\t\t};")
    w("/* End PBXNativeTarget section */")

    # PBXProject
    w("\n/* Begin PBXProject section */")
    w(f"\t\t{id_project} /* Project object */ = {{")
    w("\t\t\tisa = PBXProject;")
    w("\t\t\tattributes = {")
    w("\t\t\t\tBuildIndependentTargetsInParallel = 1;")
    w("\t\t\t\tLastSwiftUpdateCheck = 1520;")
    w("\t\t\t\tLastUpgradeCheck = 1520;")
    w("\t\t\t\tTargetAttributes = {")
    w(f"\t\t\t\t\t{id_target} = {{")
    w("\t\t\t\t\t\tCreatedOnToolsVersion = 15.2;")
    w("\t\t\t\t\t};")
    w("\t\t\t\t};")
    w("\t\t\t};")
    w(f"\t\t\tbuildConfigurationList = {id_build_config_list_project};")
    w('\t\t\tcompatibilityVersion = "Xcode 14.0";')
    w("\t\t\tdevelopmentRegion = en;")
    w("\t\t\thasScannedForEncodings = 0;")
    w("\t\t\tknownRegions = (")
    w("\t\t\t\ten,")
    w("\t\t\t\tBase,")
    w("\t\t\t\tzh-Hans,")
    w("\t\t\t);")
    w(f"\t\t\tmainGroup = {id_main_group};")
    w(f"\t\t\tproductRefGroup = {id_products_group} /* Products */;")
    w('\t\t\tprojectDirPath = "";')
    w('\t\t\tprojectRoot = "";')
    w("\t\t\ttargets = (")
    w(f"\t\t\t\t{id_target} /* {PRODUCT_NAME} */,")
    w("\t\t\t);")
    w("\t\t};")
    w("/* End PBXProject section */")

    # PBXResourcesBuildPhase
    w("\n/* Begin PBXResourcesBuildPhase section */")
    w(f"\t\t{id_resources_phase} /* Resources */ = {{")
    w("\t\t\tisa = PBXResourcesBuildPhase;")
    w("\t\t\tbuildActionMask = 2147483647;")
    w("\t\t\tfiles = (")
    w("\t\t\t);")
    w("\t\t\trunOnlyForDeploymentPostprocessing = 0;")
    w("\t\t};")
    w("/* End PBXResourcesBuildPhase section */")

    # PBXSourcesBuildPhase
    w("\n/* Begin PBXSourcesBuildPhase section */")
    w(f"\t\t{id_sources_phase} /* Sources */ = {{")
    w("\t\t\tisa = PBXSourcesBuildPhase;")
    w("\t\t\tbuildActionMask = 2147483647;")
    w("\t\t\tfiles = (")
    for bf, _ref in build_files:
        w(f"\t\t\t\t{bf} /* file */,")
    w("\t\t\t);")
    w("\t\t\trunOnlyForDeploymentPostprocessing = 0;")
    w("\t\t};")
    w("/* End PBXSourcesBuildPhase section */")

    # XCBuildConfiguration
    shared_project = """
				ALWAYS_SEARCH_USER_PATHS = NO;
				CLANG_ANALYZER_NONNULL = YES;
				CLANG_ENABLE_MODULES = YES;
				CLANG_ENABLE_OBJC_ARC = YES;
				CLANG_WARN_BOOL_CONVERSION = YES;
				CLANG_WARN_DOCUMENTATION_COMMENTS = YES;
				CLANG_WARN_EMPTY_BODY = YES;
				CLANG_WARN_UNREACHABLE_CODE = YES;
				COPY_PHASE_STRIP = NO;
				ENABLE_STRICT_OBJC_MSGSEND = YES;
				GCC_C_LANGUAGE_STANDARD = gnu17;
				GCC_NO_COMMON_BLOCKS = YES;
				GCC_WARN_UNDECLARED_SELECTOR = YES;
				GCC_WARN_UNINITIALIZED_AUTOS = YES_AGGRESSIVE;
				GCC_WARN_UNUSED_FUNCTION = YES;
				GCC_WARN_UNUSED_VARIABLE = YES;
				IPHONEOS_DEPLOYMENT_TARGET = {deployment};
				SDKROOT = iphoneos;
				SWIFT_VERSION = 5.0;
				ENABLE_USER_SCRIPT_SANDBOXING = NO;
"""

    def target_settings(config: str) -> str:
        opt = "-O" if config == "Release" else "-Onone"
        return f"""
				ASSETCATALOG_COMPILER_GLOBAL_ACCENT_COLOR_NAME = "";
				CODE_SIGN_STYLE = Automatic;
				CURRENT_PROJECT_VERSION = {build_number};
				DEVELOPMENT_TEAM = "";
				ENABLE_PREVIEWS = YES;
				GENERATE_INFOPLIST_FILE = NO;
				INFOPLIST_FILE = "{SOURCE_ROOT}/Info.plist";
				LD_RUNPATH_SEARCH_PATHS = (
					"$(inherited)",
					"@executable_path/Frameworks",
				);
				MARKETING_VERSION = {version};
				PRODUCT_BUNDLE_IDENTIFIER = {BUNDLE_ID};
				PRODUCT_NAME = "$(TARGET_NAME)";
				SWIFT_EMIT_LOC_STRINGS = YES;
				SWIFT_OPTIMIZATION_LEVEL = "{opt}";
				TARGETED_DEVICE_FAMILY = "1,2";
"""

    w("\n/* Begin XCBuildConfiguration section */")
    for cid, name, extra in [
        (id_config_project_debug, "Debug",
         shared_project.replace("{deployment}", deployment) +
         "\t\t\t\tDEBUG_INFORMATION_FORMAT = dwarf;\n"
         "\t\t\t\tENABLE_TESTABILITY = YES;\n"
         "\t\t\t\tGCC_OPTIMIZATION_LEVEL = 0;\n"
         "\t\t\t\tGCC_PREPROCESSOR_DEFINITIONS = (\n"
         '\t\t\t\t\t"DEBUG=1",\n'
         '\t\t\t\t\t"$(inherited)",\n'
         "\t\t\t\t);\n"
         "\t\t\t\tONLY_ACTIVE_ARCH = YES;\n"
         "\t\t\t\tSWIFT_ACTIVE_COMPILATION_CONDITIONS = DEBUG;\n"
         "\t\t\t\tSWIFT_OPTIMIZATION_LEVEL = \"-Onone\";\n"),
        (id_config_project_release, "Release",
         shared_project.replace("{deployment}", deployment) +
         "\t\t\t\tDEBUG_INFORMATION_FORMAT = \"dwarf-with-dsym\";\n"
         "\t\t\t\tENABLE_NS_ASSERTIONS = NO;\n"
         "\t\t\t\tSWIFT_COMPILATION_MODE = wholemodule;\n"),
    ]:
        w(f"\t\t{cid} /* {name} */ = {{")
        w("\t\t\tisa = XCBuildConfiguration;")
        w("\t\t\tbuildSettings = {")
        w(extra.rstrip("\n"))
        w("\t\t\t};")
        w(f"\t\t\tname = {name};")
        w("\t\t};")

    for cid, name in [
        (id_config_target_debug, "Debug"),
        (id_config_target_release, "Release"),
    ]:
        w(f"\t\t{cid} /* {name} */ = {{")
        w("\t\t\tisa = XCBuildConfiguration;")
        w("\t\t\tbuildSettings = {")
        w(target_settings(name).rstrip("\n"))
        w("\t\t\t};")
        w(f"\t\t\tname = {name};")
        w("\t\t};")
    w("/* End XCBuildConfiguration section */")

    # XCConfigurationList
    w("\n/* Begin XCConfigurationList section */")
    for list_id, debug_id, release_id, owner in [
        (id_build_config_list_project, id_config_project_debug,
         id_config_project_release, "PBXProject"),
        (id_build_config_list_target, id_config_target_debug,
         id_config_target_release, "PBXNativeTarget"),
    ]:
        w(f"\t\t{list_id} /* Build configuration list */ = {{")
        w("\t\t\tisa = XCConfigurationList;")
        w("\t\t\tbuildConfigurations = (")
        w(f"\t\t\t\t{debug_id} /* Debug */,")
        w(f"\t\t\t\t{release_id} /* Release */,")
        w("\t\t\t);")
        w("\t\t\tdefaultConfigurationIsVisible = 0;")
        w("\t\t\tdefaultConfigurationName = Release;")
        w("\t\t};")
    w("/* End XCConfigurationList section */")

    w("\t};")
    w(f"\trootObject = {id_project} /* Project object */;")
    w("}")
    return "\n".join(out) + "\n"


# --------------------------------------------------------------------------- #
# Shared scheme (so `xcodebuild -scheme PhotoSync` works)
# --------------------------------------------------------------------------- #
SCHEME = """<?xml version="1.0" encoding="UTF-8"?>
<Scheme LastUpgradeVersion = "1520" version = "1.7">
   <BuildAction parallelizeBuildables = "YES" buildImplicitDependencies = "YES">
      <BuildActionEntries>
         <BuildActionEntry buildForTesting = "YES" buildForRunning = "YES"
                           buildForProfiling = "YES" buildForArchiving = "YES"
                           buildForAnalyzing = "YES">
            <BuildableReference
               BuildableIdentifier = "primary"
               BlueprintIdentifier = "{target_id}"
               BuildableName = "{product}.app"
               BlueprintName = "{product}"
               ReferencedContainer = "container:{project}">
            </BuildableReference>
         </BuildActionEntry>
      </BuildActionEntries>
   </BuildAction>
   <TestAction buildConfiguration = "Debug"
               selectedDebuggerIdentifier = "Xcode.DebuggerFoundation.Debugger.LLDB"
               selectedLauncherIdentifier = "Xcode.DebuggerFoundation.Launcher.LLDB"
               shouldUseLaunchSchemeArgsEnv = "YES">
      <Testables>
      </Testables>
   </TestAction>
   <LaunchAction buildConfiguration = "Debug"
                 selectedDebuggerIdentifier = "Xcode.DebuggerFoundation.Debugger.LLDB"
                 selectedLauncherIdentifier = "Xcode.DebuggerFoundation.Launcher.LLDB"
                 launchStyle = "0" useCustomWorkingDirectory = "NO"
                 ignoresPersistentStateOnLaunch = "NO" debugDocumentVersioning = "YES"
                 debugServiceExtension = "internal" allowLocationSimulation = "YES">
      <BuildableProductRunnable runnableDebuggingMode = "0">
         <BuildableReference
            BuildableIdentifier = "primary"
            BlueprintIdentifier = "{target_id}"
            BuildableName = "{product}.app"
            BlueprintName = "{product}"
            ReferencedContainer = "container:{project}">
         </BuildableReference>
      </BuildableProductRunnable>
   </LaunchAction>
   <ProfileAction buildConfiguration = "Release"
                  shouldUseLaunchSchemeArgsEnv = "YES" savedToolIdentifier = ""
                  useCustomWorkingDirectory = "NO" debugDocumentVersioning = "YES">
      <BuildableProductRunnable runnableDebuggingMode = "0">
         <BuildableReference
            BuildableIdentifier = "primary"
            BlueprintIdentifier = "{target_id}"
            BuildableName = "{product}.app"
            BlueprintName = "{product}"
            ReferencedContainer = "container:{project}">
         </BuildableReference>
      </BuildableProductRunnable>
   </ProfileAction>
   <AnalyzeAction buildConfiguration = "Debug">
   </AnalyzeAction>
   <ArchiveAction buildConfiguration = "Release" revealArchiveInOrganizer = "YES">
   </ArchiveAction>
</Scheme>
"""

PRODUCT_NAME = "PhotoSync"
BUNDLE_ID = "dev.photosync"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", default="PhotoSync.xcodeproj")
    parser.add_argument("--deployment", default="15.0")
    parser.add_argument("--version", default=None)
    parser.add_argument("--build-number", default="1")
    args = parser.parse_args()

    version = args.version
    if version is None:
        version_file = os.path.join("..", "VERSION")
        if os.path.isfile(version_file):
            with open(version_file) as handle:
                version = handle.read().strip()
        version = version or "1.0.0"

    if not os.path.isdir(SOURCE_ROOT):
        print(f"✗ 请在 ios/ 目录下运行（找不到 {SOURCE_ROOT}/）", file=sys.stderr)
        return 1

    groups = discover_sources()
    total = sum(len(v) for v in groups.values())
    if total == 0:
        print("✗ 没有找到任何 Swift 源文件", file=sys.stderr)
        return 1

    out = args.output
    if os.path.isdir(out):
        shutil.rmtree(out)
    os.makedirs(os.path.join(out, "xcshareddata", "xcschemes"), exist_ok=True)

    pbxproj = build(groups, args.deployment, version, args.build_number)
    with open(os.path.join(out, "project.pbxproj"), "w") as handle:
        handle.write(pbxproj)

    target_id = stable_id("target")
    scheme = SCHEME.format(target_id=target_id, product=PRODUCT_NAME, project=out)
    with open(os.path.join(out, "xcshareddata", "xcschemes", f"{PRODUCT_NAME}.xcscheme"),
              "w") as handle:
        handle.write(scheme)

    print(f"✓ 已生成 {out}")
    print(f"  版本 {version} ({args.build_number}) · iOS {args.deployment}+")
    for group, files in groups.items():
        print(f"  {group}/: {len(files)} 个文件")
    print(f"  共 {total} 个 Swift 文件")
    return 0


if __name__ == "__main__":
    sys.exit(main())
