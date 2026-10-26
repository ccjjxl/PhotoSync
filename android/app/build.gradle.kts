plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

import java.util.Properties

// ---------------------------------------------------------------------------
// Release signing
//
// Credentials live in android/keystore.properties (git-ignored). Create it with
//   ./scripts/gen-keystore.sh
// When the file is absent (CI, fresh clone) the release build falls back to the
// debug key so `./gradlew assembleRelease` still produces an installable APK.
// ---------------------------------------------------------------------------
val keystorePropsFile = rootProject.file("keystore.properties")
val keystoreProps = Properties().apply {
    if (keystorePropsFile.exists()) {
        keystorePropsFile.inputStream().use { load(it) }
    }
}
val hasReleaseKeystore = keystorePropsFile.exists() &&
    keystoreProps.getProperty("storeFile")?.let { rootProject.file(it).exists() } == true

// Version resolution order:
//   1. -PversionName / -PversionCode   (explicit override, used by release CI)
//   2. keystore.properties
//   3. the repo's VERSION file        (single source of truth)
//   4. hard-coded fallback
val repoVersionFile = rootProject.file("../VERSION")
val repoVersion = if (repoVersionFile.exists()) repoVersionFile.readText().trim() else ""
val appVersionName = (findProperty("versionName") as String?)
    ?: repoVersion.ifEmpty { keystoreProps.getProperty("versionName") ?: "1.0.0" }
// Android requires a monotonically increasing integer; derive a stable one from
// the semver so every release gets a distinct, ordered code without hand-editing.
val appVersionCode = (
    (findProperty("versionCode") as String?)
        ?: if (repoVersion.isNotEmpty()) deriveVersionCode(appVersionName)
        else keystoreProps.getProperty("versionCode")
        ?: deriveVersionCode(appVersionName)
    ).toInt()

fun deriveVersionCode(name: String): String {
    val parts = name.split(".").mapNotNull { it.toIntOrNull() }
    val major = parts.getOrNull(0) ?: 1
    val minor = parts.getOrNull(1) ?: 0
    val patch = parts.getOrNull(2) ?: 0
    return (major * 10000 + minor * 100 + patch).toString()
}

android {
    namespace = "dev.photosync"
    compileSdk = 36

    defaultConfig {
        applicationId = "dev.photosync"
        minSdk = 26
        targetSdk = 36
        versionCode = appVersionCode
        versionName = appVersionName
        resourceConfigurations += listOf("zh", "en")
    }

    signingConfigs {
        if (hasReleaseKeystore) {
            create("release") {
                storeFile = rootProject.file(keystoreProps.getProperty("storeFile"))
                storePassword = keystoreProps.getProperty("storePassword")
                keyAlias = keystoreProps.getProperty("keyAlias")
                keyPassword = keystoreProps.getProperty("keyPassword")
                enableV1Signing = true   // Android 6 and older
                enableV2Signing = true
                enableV3Signing = true
                enableV4Signing = false  // not needed for sideloading, keeps APK small
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
            signingConfig = if (hasReleaseKeystore) {
                signingConfigs.getByName("release")
            } else {
                logger.warn(
                    "⚠ keystore.properties 不存在，release 将使用 debug 签名。" +
                        "运行 ./scripts/gen-keystore.sh 生成正式签名。",
                )
                signingConfigs.getByName("debug")
            }
        }
        debug {
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-debug"
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlin {
        compilerOptions {
            jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    packaging {
        resources.excludes += setOf("/META-INF/{AL2.0,LGPL2.1}")
    }

    // Deterministic, versioned output names for easy distribution.
    applicationVariants.all {
        outputs.all {
            val out = this as com.android.build.gradle.internal.api.BaseVariantOutputImpl
            val variantName = name
            val suffix = if (variantName == "release") "" else "-$variantName"
            out.outputFileName = "PhotoSync-${appVersionName}${suffix}.apk"
        }
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.18.0")
    implementation("androidx.activity:activity-compose:1.13.0")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.9.4")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.9.4")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.9.4")
    implementation("androidx.lifecycle:lifecycle-service:2.9.4")
    implementation("androidx.exifinterface:exifinterface:1.3.6")

    implementation(platform("androidx.compose:compose-bom:2025.10.01"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-core")

    debugImplementation("androidx.compose.ui:ui-tooling")
}
