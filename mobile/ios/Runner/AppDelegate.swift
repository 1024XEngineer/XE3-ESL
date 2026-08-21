import AVFoundation
import Flutter
import UIKit

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
  private var agentPCMPlayer: AgentPCMStreamPlayer?
  private var nativeTabBarFactory: NativeTabBarFactory?

  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
    GeneratedPluginRegistrant.register(with: engineBridge.pluginRegistry)
    let tabBarFactory = NativeTabBarFactory(
      messenger: engineBridge.applicationRegistrar.messenger()
    )
    guard let nativeTabBarRegistrar = engineBridge.pluginRegistry.registrar(
      forPlugin: "NativeTabBarPlugin"
    ) else {
      fatalError("Unable to register the native iOS tab bar.")
    }
    nativeTabBarRegistrar.register(
      tabBarFactory,
      withId: "speakup/native_tab_bar"
    )
    nativeTabBarFactory = tabBarFactory
    agentPCMPlayer = AgentPCMStreamPlayer(
      messenger: engineBridge.applicationRegistrar.messenger()
    )
  }
}

private final class NativeTabBarFactory: NSObject, FlutterPlatformViewFactory {
  private let messenger: FlutterBinaryMessenger

  init(messenger: FlutterBinaryMessenger) {
    self.messenger = messenger
    super.init()
  }

  func createArgsCodec() -> FlutterMessageCodec & NSObjectProtocol {
    FlutterStandardMessageCodec.sharedInstance()
  }

  func create(
    withFrame frame: CGRect,
    viewIdentifier viewId: Int64,
    arguments args: Any?
  ) -> FlutterPlatformView {
    NativeTabBarView(
      frame: frame,
      viewId: viewId,
      arguments: args,
      messenger: messenger
    )
  }
}

private final class NativeTabBarView: NSObject, FlutterPlatformView, UITabBarDelegate {
  private let tabBar: UITabBar
  private let channel: FlutterMethodChannel

  init(
    frame: CGRect,
    viewId: Int64,
    arguments: Any?,
    messenger: FlutterBinaryMessenger
  ) {
    tabBar = UITabBar(frame: frame)
    channel = FlutterMethodChannel(
      name: "speakup/native_tab_bar/\(viewId)",
      binaryMessenger: messenger
    )
    super.init()

    let values = arguments as? [String: Any]
    let itemValues = values?["items"] as? [[String: String]] ?? []
    tabBar.items = itemValues.enumerated().map { index, item in
      guard
        let label = item["label"],
        let systemImageName = item["systemImage"],
        let selectedSystemImageName = item["selectedSystemImage"],
        let image = UIImage(systemName: systemImageName),
        let selectedImage = UIImage(systemName: selectedSystemImageName)
      else {
        fatalError("Invalid native tab bar item configuration.")
      }
      let tabBarItem = UITabBarItem(
        title: label,
        image: image,
        selectedImage: selectedImage
      )
      tabBarItem.tag = index
      return tabBarItem
    }
    let selectedColor = UIColor(red: 20 / 255, green: 32 / 255, blue: 51 / 255, alpha: 1)
    let unselectedColor = UIColor(red: 138 / 255, green: 148 / 255, blue: 166 / 255, alpha: 1)
    tabBar.tintColor = selectedColor
    tabBar.unselectedItemTintColor = unselectedColor
    let appearance = UITabBarAppearance()
    appearance.configureWithTransparentBackground()
    appearance.backgroundEffect = UIBlurEffect(style: .systemChromeMaterial)
    appearance.backgroundColor = UIColor.white.withAlphaComponent(0.78)
    appearance.shadowColor = UIColor(
      red: 151 / 255,
      green: 166 / 255,
      blue: 189 / 255,
      alpha: 0.16
    )
    appearance.stackedLayoutAppearance.normal.iconColor = unselectedColor
    appearance.stackedLayoutAppearance.normal.titleTextAttributes = [
      .foregroundColor: unselectedColor,
      .font: UIFont.systemFont(ofSize: 10, weight: .semibold),
    ]
    appearance.stackedLayoutAppearance.selected.iconColor = selectedColor
    appearance.stackedLayoutAppearance.selected.titleTextAttributes = [
      .foregroundColor: selectedColor,
      .font: UIFont.systemFont(ofSize: 10, weight: .semibold),
    ]
    tabBar.standardAppearance = appearance
    tabBar.scrollEdgeAppearance = appearance
    tabBar.isTranslucent = true
    select(index: values?["selectedIndex"] as? Int ?? 0)
    tabBar.delegate = self

    channel.setMethodCallHandler { [weak self] call, result in
      guard call.method == "setSelectedIndex", let index = call.arguments as? Int else {
        result(FlutterMethodNotImplemented)
        return
      }
      self?.select(index: index)
      result(nil)
    }
  }

  deinit {
    channel.setMethodCallHandler(nil)
  }

  func view() -> UIView {
    tabBar
  }

  func tabBar(_ tabBar: UITabBar, didSelect item: UITabBarItem) {
    channel.invokeMethod("onSelected", arguments: item.tag) { [weak self] result in
      guard
        tabBar.selectedItem?.tag == item.tag,
        let selectedIndex = result as? Int
      else {
        return
      }
      self?.select(index: selectedIndex)
    }
  }

  private func select(index: Int) {
    guard let items = tabBar.items, items.indices.contains(index) else {
      return
    }
    tabBar.selectedItem = items[index]
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
      standardFormatWithSampleRate: 24_000,
      channels: 1
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
        standardFormatWithSampleRate: 24_000,
        channels: 1
      ),
      let buffer = AVAudioPCMBuffer(
        pcmFormat: format,
        frameCapacity: AVAudioFrameCount(data.count / 2)
      ),
      let samples = buffer.floatChannelData?[0]
    else {
      throw PlayerError.invalidState
    }
    buffer.frameLength = buffer.frameCapacity
    data.withUnsafeBytes { rawBuffer in
      let bytes = rawBuffer.bindMemory(to: UInt8.self)
      for frame in 0..<Int(buffer.frameLength) {
        let offset = frame * 2
        let value = UInt16(bytes[offset]) | (UInt16(bytes[offset + 1]) << 8)
        samples[frame] = Float(Int16(bitPattern: value)) / 32_768
      }
    }
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
    try? AVAudioSession.sharedInstance().setActive(
      false,
      options: .notifyOthersOnDeactivation
    )
  }

  private enum PlayerError: Error {
    case invalidArguments
    case invalidState
  }
}
