package com.xengineer.speakup

import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioTrack
import android.media.PlaybackParams
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.embedding.android.FlutterActivity
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger

class MainActivity : FlutterActivity() {
    private val executor = Executors.newSingleThreadExecutor()
    private val generation = AtomicInteger()

    @Volatile
    private var track: AudioTrack? = null
    private var writtenFrames = 0L

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "speakup/agent_pcm_player",
        ).setMethodCallHandler(::handlePCMCall)
    }

    override fun cleanUpFlutterEngine(flutterEngine: FlutterEngine) {
        stopNative()
        super.cleanUpFlutterEngine(flutterEngine)
    }

    private fun handlePCMCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "start" -> start(call, result)
            "append" -> append(call, result)
            "finish" -> finish(result)
            "stop" -> {
                stopNative()
                result.success(null)
            }
            else -> result.notImplemented()
        }
    }

    private fun start(call: MethodCall, result: MethodChannel.Result) {
        val sampleRate = call.argument<Int>("sampleRate")
        val channelCount = call.argument<Int>("channelCount")
        val bitsPerSample = call.argument<Int>("bitsPerSample")
        val speed = call.argument<Double>("speed")
        if (sampleRate != 24_000 || channelCount != 1 || bitsPerSample != 16 ||
            speed == null || speed < 0.5 || speed > 2.0
        ) {
            result.error("agent_pcm_playback_failed", "Invalid PCM format.", null)
            return
        }
        stopNative()
        val current = generation.incrementAndGet()
        executor.execute {
            try {
                val minimum = AudioTrack.getMinBufferSize(
                    sampleRate,
                    AudioFormat.CHANNEL_OUT_MONO,
                    AudioFormat.ENCODING_PCM_16BIT,
                )
                val nextTrack = AudioTrack.Builder()
                    .setAudioAttributes(
                        AudioAttributes.Builder()
                            .setUsage(AudioAttributes.USAGE_ASSISTANCE_ACCESSIBILITY)
                            .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                            .build(),
                    )
                    .setAudioFormat(
                        AudioFormat.Builder()
                            .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                            .setSampleRate(sampleRate)
                            .setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                            .build(),
                    )
                    .setBufferSizeInBytes(maxOf(minimum, sampleRate))
                    .setTransferMode(AudioTrack.MODE_STREAM)
                    .build()
                nextTrack.playbackParams = PlaybackParams().setSpeed(speed.toFloat())
                nextTrack.play()
                if (generation.get() != current) {
                    nextTrack.release()
                    postSuccess(result)
                    return@execute
                }
                track = nextTrack
                writtenFrames = 0
                postSuccess(result)
            } catch (_: Exception) {
                postFailure(result)
            }
        }
    }

    private fun append(call: MethodCall, result: MethodChannel.Result) {
        val audio = call.arguments as? ByteArray
        if (audio == null || audio.isEmpty() || audio.size % 2 != 0) {
            result.error("agent_pcm_playback_failed", "Invalid PCM audio.", null)
            return
        }
        val current = generation.get()
        executor.execute {
            val active = track
            if (active == null || generation.get() != current) {
                postFailure(result)
                return@execute
            }
            val written = active.write(audio, 0, audio.size, AudioTrack.WRITE_BLOCKING)
            if (written != audio.size) {
                postFailure(result)
                return@execute
            }
            writtenFrames += written / 2
            audio.fill(0)
            postSuccess(result)
        }
    }

    private fun finish(result: MethodChannel.Result) {
        val current = generation.get()
        executor.execute {
            val active = track
            if (active == null) {
                postFailure(result)
                return@execute
            }
            while (generation.get() == current &&
                active.playbackHeadPosition.toLong() < writtenFrames
            ) {
                Thread.sleep(10)
            }
            if (generation.get() == current) {
                stopNative()
            }
            postSuccess(result)
        }
    }

    private fun stopNative() {
        generation.incrementAndGet()
        val active = track
        track = null
        writtenFrames = 0
        try {
            active?.pause()
            active?.flush()
            active?.release()
        } catch (_: IllegalStateException) {
            // The AudioTrack is already stopped.
        }
    }

    private fun postSuccess(result: MethodChannel.Result) {
        runOnUiThread { result.success(null) }
    }

    private fun postFailure(result: MethodChannel.Result) {
        runOnUiThread {
            result.error("agent_pcm_playback_failed", "PCM playback failed.", null)
        }
    }
}
