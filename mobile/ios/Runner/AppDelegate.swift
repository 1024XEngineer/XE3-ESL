import AVFoundation
import Flutter
import UIKit

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
  private var agentPCMPlayer: AgentPCMStreamPlayer?

  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
    GeneratedPluginRegistrant.register(with: engineBridge.pluginRegistry)
    agentPCMPlayer = AgentPCMStreamPlayer(
      messenger: engineBridge.applicationRegistrar.messenger()
    )
  }
}

private final class AgentPCMStreamPlayer {
  private let channel: FlutterMethodChannel
  private var engine: AVAudioEngine?
  private var player: AVAudioPlayerNode?
  private var pendingBuffers = 0
  private var finishResult: FlutterResult?

  init(messenger: FlutterBinaryMessenger) {
    channel = FlutterMethodChannel(
      name: "speakup/agent_pcm_player",
      binaryMessenger: messenger
    )
    channel.setMethodCallHandler { [weak self] call, result in
      self?.handle(call, result: result)
    }
  }

  private func handle(_ call: FlutterMethodCall, result: @escaping FlutterResult) {
    do {
      switch call.method {
      case "start":
        guard
          let arguments = call.arguments as? [String: Any],
          let sampleRate = arguments["sampleRate"] as? NSNumber,
          let channelCount = arguments["channelCount"] as? NSNumber,
          let bitsPerSample = arguments["bitsPerSample"] as? NSNumber,
          let speed = arguments["speed"] as? NSNumber,
          sampleRate.intValue == 24_000,
          channelCount.intValue == 1,
          bitsPerSample.intValue == 16,
          speed.doubleValue >= 0.5,
          speed.doubleValue <= 2
        else {
          throw PlayerError.invalidArguments
        }
        try start(speed: speed.floatValue)
        result(nil)
      case "append":
        guard let data = call.arguments as? FlutterStandardTypedData else {
          throw PlayerError.invalidArguments
        }
        try append(data.data)
        result(nil)
      case "finish":
        guard player != nil, finishResult == nil else {
          throw PlayerError.invalidState
        }
        if pendingBuffers == 0 {
          stopNative()
          result(nil)
        } else {
          finishResult = result
        }
      case "stop":
        stopNative()
        result(nil)
      default:
        result(FlutterMethodNotImplemented)
      }
    } catch {
      result(
        FlutterError(
          code: "agent_pcm_playback_failed",
          message: "PCM playback failed.",
          details: nil
        )
      )
    }
  }

  private func start(speed: Float) throws {
    stopNative()
    let session = AVAudioSession.sharedInstance()
    try session.setCategory(
      .playAndRecord,
      mode: .spokenAudio,
      options: [.defaultToSpeaker, .allowBluetoothHFP]
    )
    try session.setActive(true)
    guard let format = AVAudioFormat(
      commonFormat: .pcmFormatInt16,
      sampleRate: 24_000,
      channels: 1,
      interleaved: true
    ) else {
      throw PlayerError.invalidState
    }
    let nextEngine = AVAudioEngine()
    let nextPlayer = AVAudioPlayerNode()
    let timePitch = AVAudioUnitTimePitch()
    timePitch.rate = speed
    nextEngine.attach(nextPlayer)
    nextEngine.attach(timePitch)
    nextEngine.connect(nextPlayer, to: timePitch, format: format)
    nextEngine.connect(timePitch, to: nextEngine.mainMixerNode, format: format)
    try nextEngine.start()
    nextPlayer.play()
    engine = nextEngine
    player = nextPlayer
  }

  private func append(_ data: Data) throws {
    guard
      let player,
      data.count > 0,
      data.count.isMultiple(of: 2),
      let format = AVAudioFormat(
        commonFormat: .pcmFormatInt16,
        sampleRate: 24_000,
        channels: 1,
        interleaved: true
      ),
      let buffer = AVAudioPCMBuffer(
        pcmFormat: format,
        frameCapacity: AVAudioFrameCount(data.count / 2)
      )
    else {
      throw PlayerError.invalidState
    }
    buffer.frameLength = buffer.frameCapacity
    let audioBuffer = buffer.mutableAudioBufferList.pointee.mBuffers
    guard let destination = audioBuffer.mData else {
      throw PlayerError.invalidState
    }
    data.copyBytes(to: destination.assumingMemoryBound(to: UInt8.self), count: data.count)
    buffer.mutableAudioBufferList.pointee.mBuffers.mDataByteSize = UInt32(data.count)
    pendingBuffers += 1
    player.scheduleBuffer(buffer, completionCallbackType: .dataPlayedBack) {
      [weak self] _ in
      DispatchQueue.main.async {
        self?.didPlayBuffer()
      }
    }
  }

  private func didPlayBuffer() {
    pendingBuffers = max(0, pendingBuffers - 1)
    guard pendingBuffers == 0, let result = finishResult else {
      return
    }
    finishResult = nil
    stopNative()
    result(nil)
  }

  private func stopNative() {
    player?.stop()
    engine?.stop()
    player = nil
    engine = nil
    pendingBuffers = 0
    if let result = finishResult {
      finishResult = nil
      result(nil)
    }
  }

  private enum PlayerError: Error {
    case invalidArguments
    case invalidState
  }
}
