plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

val releaseSigningVariableNames = listOf(
    "SPEAKUP_ANDROID_KEYSTORE_PATH",
    "SPEAKUP_ANDROID_KEY_ALIAS",
    "SPEAKUP_ANDROID_STORE_PASSWORD",
    "SPEAKUP_ANDROID_KEY_PASSWORD",
)
val releaseBuildRequested = gradle.startParameter.taskNames.any {
    it.contains("release", ignoreCase = true)
}
val releaseSigningValues = releaseSigningVariableNames.associateWith(System::getenv)
val missingReleaseSigningVariables = releaseSigningValues
    .filterValues { it.isNullOrBlank() }
    .keys

if (releaseBuildRequested && missingReleaseSigningVariables.isNotEmpty()) {
    throw GradleException(
        "Missing Android release signing environment variables: " +
            missingReleaseSigningVariables.joinToString(", "),
    )
}

val releaseKeystore = releaseSigningValues["SPEAKUP_ANDROID_KEYSTORE_PATH"]
    ?.takeIf { it.isNotBlank() }
    ?.let(::file)
if (releaseBuildRequested && releaseKeystore?.isFile != true) {
    throw GradleException("Android release keystore is not a readable file.")
}

android {
    namespace = "com.xengineer.speakup"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "com.xengineer.speakup"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = 24
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        ndk {
            abiFilters += "arm64-v8a"
        }
    }

    packaging {
        jniLibs {
            excludes += listOf("lib/armeabi-v7a/**", "lib/x86/**", "lib/x86_64/**")
        }
    }

    signingConfigs {
        if (missingReleaseSigningVariables.isEmpty()) {
            create("release") {
                storeFile = releaseKeystore
                keyAlias = releaseSigningValues.getValue("SPEAKUP_ANDROID_KEY_ALIAS")
                storePassword = releaseSigningValues.getValue("SPEAKUP_ANDROID_STORE_PASSWORD")
                keyPassword = releaseSigningValues.getValue("SPEAKUP_ANDROID_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.findByName("release")
        }
    }

    flavorDimensions += "environment"
    productFlavors {
        create("staging") {
            dimension = "environment"
        }
        create("production") {
            dimension = "environment"
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}
