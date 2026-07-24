import Flutter
import Security
import UIKit
import XCTest

class RunnerTests: XCTestCase {

  func testKeychainRoundTrip() {
    let service = "com.xe3-esl.speakup.identity"
    let account = "session_token_simulator_probe"
    let value = Data("sess_simulator-roundtrip".utf8)
    let baseQuery: [CFString: Any] = [
      kSecClass: kSecClassGenericPassword,
      kSecAttrService: service,
      kSecAttrAccount: account,
    ]

    SecItemDelete(baseQuery as CFDictionary)
    defer {
      SecItemDelete(baseQuery as CFDictionary)
    }

    var addQuery = baseQuery
    addQuery[kSecValueData] = value
    addQuery[kSecAttrAccessible] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
    XCTAssertEqual(SecItemAdd(addQuery as CFDictionary, nil), errSecSuccess)

    var readQuery = baseQuery
    readQuery[kSecReturnData] = true
    readQuery[kSecMatchLimit] = kSecMatchLimitOne
    var result: CFTypeRef?
    XCTAssertEqual(
      SecItemCopyMatching(readQuery as CFDictionary, &result),
      errSecSuccess
    )
    XCTAssertEqual(result as? Data, value)

    XCTAssertEqual(SecItemDelete(baseQuery as CFDictionary), errSecSuccess)
  }

}
