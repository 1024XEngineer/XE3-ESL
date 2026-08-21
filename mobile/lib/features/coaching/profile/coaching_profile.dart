import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

enum CoachingResponseDetail { concise, balanced, detailed }

final class CoachingProfileData {
  const CoachingProfileData({
    this.formOfAddress = '',
    this.occupation = '',
    this.professionalContext = '',
    this.nativeLanguage = '',
    this.explanationLanguage = '',
    this.responseDetail,
    this.interests = const <String>[],
  });

  final String formOfAddress;
  final String occupation;
  final String professionalContext;
  final String nativeLanguage;
  final String explanationLanguage;
  final CoachingResponseDetail? responseDetail;
  final List<String> interests;

  bool get isEmpty =>
      formOfAddress.isEmpty &&
      occupation.isEmpty &&
      professionalContext.isEmpty &&
      nativeLanguage.isEmpty &&
      explanationLanguage.isEmpty &&
      responseDetail == null &&
      interests.isEmpty;
}

final class CoachingProfile {
  const CoachingProfile({
    required this.memoryEnabled,
    required this.data,
    required this.version,
  });

  final bool memoryEnabled;
  final CoachingProfileData data;
  final int version;
}

abstract interface class CoachingProfileClient {
  Future<CoachingProfile> getProfile();

  Future<CoachingProfile> updateProfile({
    required int expectedVersion,
    CoachingProfileData? updates,
    List<String> forgetFields,
    bool clearProfile,
    bool? memoryEnabled,
  });
}

final class WireCoachingProfileClient implements CoachingProfileClient {
  factory WireCoachingProfileClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
  }) => WireCoachingProfileClient._(
    baseUri,
    SessionAuthenticatedHttpTransport(
      transport: transport ?? IoIdentityHttpTransport(),
      credentialProvider: credentialProvider,
      invalidateSession: invalidateSession,
      trustedBaseUri: baseUri,
    ),
  );

  WireCoachingProfileClient._(this._baseUri, this._transport);

  final Uri _baseUri;
  final IdentityHttpTransport _transport;

  @override
  Future<CoachingProfile> getProfile() => _send('GET');

  @override
  Future<CoachingProfile> updateProfile({
    required int expectedVersion,
    CoachingProfileData? updates,
    List<String> forgetFields = const <String>[],
    bool clearProfile = false,
    bool? memoryEnabled,
  }) {
    final body = <String, Object>{'expected_version': expectedVersion};
    if (updates != null) body['updates'] = _encodeData(updates);
    if (forgetFields.isNotEmpty) body['forget_fields'] = forgetFields;
    if (clearProfile) body['clear_profile'] = true;
    if (memoryEnabled != null) body['memory_enabled'] = memoryEnabled;
    return _send('PATCH', body: jsonEncode(body));
  }

  Future<CoachingProfile> _send(String method, {String? body}) async {
    final response = await _transport
        .send(
          method: method,
          uri: _baseUri.resolve('/v1/me/coaching-profile'),
          headers: <String, String>{
            HttpHeaders.acceptHeader: 'application/json',
            if (body != null) HttpHeaders.contentTypeHeader: 'application/json',
          },
          body: body,
        )
        .timeout(const Duration(seconds: 15));
    if (response.statusCode != HttpStatus.ok) {
      throw StateError('coaching profile request failed');
    }
    return _decodeProfile(response.body);
  }
}

final class CoachingProfileController extends ChangeNotifier {
  factory CoachingProfileController({required CoachingProfileClient client}) =>
      CoachingProfileController._(client);

  CoachingProfileController._(this._client);

  final CoachingProfileClient _client;
  CoachingProfile? _profile;
  bool _loading = false;
  bool _saving = false;
  String? _errorMessage;

  CoachingProfile? get profile => _profile;
  bool get loading => _loading;
  bool get saving => _saving;
  String? get errorMessage => _errorMessage;

  Future<void> clearPrivateState() async {
    _profile = null;
    _loading = false;
    _saving = false;
    _errorMessage = null;
    notifyListeners();
  }

  Future<void> load() async {
    if (_loading) return;
    _loading = true;
    _errorMessage = null;
    notifyListeners();
    try {
      _profile = await _client.getProfile();
    } on Object {
      _errorMessage = '记忆暂时无法读取，请重试。';
    } finally {
      _loading = false;
      notifyListeners();
    }
  }

  Future<bool> save(CoachingProfileData next) async {
    final current = _profile;
    if (current == null || _saving) return false;
    final updates = <String, Object>{};
    final forget = <String>[];
    void compare(String field, String before, String after) {
      final value = after.trim();
      if (value == before) return;
      if (value.isEmpty) {
        forget.add(field);
      } else {
        updates[field] = value;
      }
    }

    compare('form_of_address', current.data.formOfAddress, next.formOfAddress);
    compare('occupation', current.data.occupation, next.occupation);
    compare(
      'professional_context',
      current.data.professionalContext,
      next.professionalContext,
    );
    compare(
      'native_language',
      current.data.nativeLanguage,
      next.nativeLanguage,
    );
    compare(
      'explanation_language',
      current.data.explanationLanguage,
      next.explanationLanguage,
    );
    if (next.responseDetail != current.data.responseDetail) {
      if (next.responseDetail == null) {
        forget.add('response_detail');
      } else {
        updates['response_detail'] = next.responseDetail!.name.toUpperCase();
      }
    }
    if (!listEquals(next.interests, current.data.interests)) {
      if (next.interests.isEmpty) {
        forget.add('interests');
      } else {
        updates['interests'] = next.interests;
      }
    }
    if (updates.isEmpty && forget.isEmpty) return true;
    return _update(
      updates: updates.isEmpty ? null : updates,
      forgetFields: forget,
    );
  }

  Future<bool> setMemoryEnabled(bool enabled) =>
      _update(memoryEnabled: enabled);

  Future<bool> clear() => _update(clearProfile: true);

  Future<bool> _update({
    Map<String, Object>? updates,
    List<String> forgetFields = const <String>[],
    bool clearProfile = false,
    bool? memoryEnabled,
  }) async {
    final current = _profile;
    if (current == null || _saving) return false;
    _saving = true;
    _errorMessage = null;
    notifyListeners();
    try {
      _profile = await _client.updateProfile(
        expectedVersion: current.version,
        updates: updates == null ? null : _dataFromPatch(updates),
        forgetFields: forgetFields,
        clearProfile: clearProfile,
        memoryEnabled: memoryEnabled,
      );
      return true;
    } on Object {
      _errorMessage = '记忆没有保存成功，请刷新后重试。';
      return false;
    } finally {
      _saving = false;
      notifyListeners();
    }
  }
}

class CoachingProfileCard extends StatefulWidget {
  const CoachingProfileCard({required this.controller, super.key});

  final CoachingProfileController controller;

  @override
  State<CoachingProfileCard> createState() => _CoachingProfileCardState();
}

class _CoachingProfileCardState extends State<CoachingProfileCard> {
  @override
  void initState() {
    super.initState();
    unawaited(widget.controller.load());
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: widget.controller,
      builder: (context, _) {
        final profile = widget.controller.profile;
        return InkWell(
          key: const Key('coaching-profile-card'),
          borderRadius: BorderRadius.circular(22),
          onTap: profile == null
              ? widget.controller.load
              : () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (_) =>
                        CoachingProfilePage(controller: widget.controller),
                  ),
                ),
          child: Container(
            padding: const EdgeInsets.all(20),
            decoration: BoxDecoration(
              border: Border.all(color: const Color(0xFFE5E5E5)),
              borderRadius: BorderRadius.circular(22),
            ),
            child: Row(
              children: [
                const Icon(Icons.psychology_alt_outlined, size: 28),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text('教练记忆', style: SpeakUpDesign.sectionTitle),
                      const SizedBox(height: 4),
                      Text(
                        widget.controller.loading
                            ? '正在读取…'
                            : profile == null
                            ? widget.controller.errorMessage ?? '点按重试'
                            : !profile.memoryEnabled
                            ? '已关闭，不会注入 Agent 对话'
                            : profile.data.isEmpty
                            ? '尚未保存称呼、职业或偏好'
                            : _profileSummary(profile.data),
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: SpeakUpDesign.body.copyWith(
                          color: SpeakUpDesign.secondary,
                        ),
                      ),
                    ],
                  ),
                ),
                const Icon(Icons.chevron_right_rounded),
              ],
            ),
          ),
        );
      },
    );
  }
}

class CoachingProfilePage extends StatefulWidget {
  const CoachingProfilePage({required this.controller, super.key});

  final CoachingProfileController controller;

  @override
  State<CoachingProfilePage> createState() => _CoachingProfilePageState();
}

class _CoachingProfilePageState extends State<CoachingProfilePage> {
  final _formKey = GlobalKey<FormState>();
  late final List<TextEditingController> _fields;
  late final TextEditingController _interests;
  CoachingResponseDetail? _detail;

  @override
  void initState() {
    super.initState();
    final data = widget.controller.profile?.data ?? const CoachingProfileData();
    _fields = [
      data.formOfAddress,
      data.occupation,
      data.professionalContext,
      data.nativeLanguage,
      data.explanationLanguage,
    ].map((value) => TextEditingController(text: value)).toList();
    _interests = TextEditingController(text: data.interests.join('、'));
    _detail = data.responseDetail;
  }

  @override
  void dispose() {
    for (final controller in _fields) {
      controller.dispose();
    }
    _interests.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final profile = widget.controller.profile!;
    return Scaffold(
      appBar: AppBar(title: const Text('教练记忆')),
      body: AnimatedBuilder(
        animation: widget.controller,
        builder: (context, _) => ListView(
          padding: const EdgeInsets.fromLTRB(20, 12, 20, 40),
          children: [
            SwitchListTile.adaptive(
              contentPadding: EdgeInsets.zero,
              title: const Text('在 Agent 对话中使用这些记忆'),
              subtitle: const Text('关闭后仍保留内容，但不会注入对话上下文。'),
              value: widget.controller.profile?.memoryEnabled ?? false,
              onChanged: widget.controller.saving
                  ? null
                  : widget.controller.setMemoryEnabled,
            ),
            const SizedBox(height: 16),
            Form(
              key: _formKey,
              child: Column(
                children: [
                  _field(_fields[0], '希望怎么称呼你', 64),
                  _field(_fields[1], '职业', 120),
                  _field(_fields[2], '职业背景', 500, maxLines: 4),
                  _field(_fields[3], '母语', 64),
                  _field(_fields[4], '讲解语言', 64),
                  DropdownButtonFormField<CoachingResponseDetail?>(
                    initialValue: _detail,
                    decoration: const InputDecoration(labelText: '回答详细程度'),
                    items: const [
                      DropdownMenuItem(value: null, child: Text('未设置')),
                      DropdownMenuItem(
                        value: CoachingResponseDetail.concise,
                        child: Text('简洁'),
                      ),
                      DropdownMenuItem(
                        value: CoachingResponseDetail.balanced,
                        child: Text('适中'),
                      ),
                      DropdownMenuItem(
                        value: CoachingResponseDetail.detailed,
                        child: Text('详细'),
                      ),
                    ],
                    onChanged: (value) => _detail = value,
                  ),
                  _field(_interests, '兴趣（用逗号或顿号分隔，最多 8 个）', 520),
                ],
              ),
            ),
            if (widget.controller.errorMessage case final error?) ...[
              const SizedBox(height: 12),
              Text(
                error,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: 24),
            FilledButton(
              onPressed: widget.controller.saving ? null : _save,
              child: Text(widget.controller.saving ? '正在保存…' : '保存'),
            ),
            TextButton(
              onPressed: widget.controller.saving || profile.data.isEmpty
                  ? null
                  : _clear,
              child: const Text('清空教练记忆'),
            ),
          ],
        ),
      ),
    );
  }

  Widget _field(
    TextEditingController controller,
    String label,
    int maxLength, {
    int maxLines = 1,
  }) => TextFormField(
    controller: controller,
    decoration: InputDecoration(labelText: label),
    maxLength: maxLength,
    maxLines: maxLines,
  );

  Future<void> _save() async {
    final interests = _interests.text
        .split(RegExp(r'[,，、\n]'))
        .map((value) => value.trim())
        .where((value) => value.isNotEmpty)
        .toSet()
        .toList();
    if (interests.length > 8) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('兴趣最多填写 8 个')));
      return;
    }
    final saved = await widget.controller.save(
      CoachingProfileData(
        formOfAddress: _fields[0].text.trim(),
        occupation: _fields[1].text.trim(),
        professionalContext: _fields[2].text.trim(),
        nativeLanguage: _fields[3].text.trim(),
        explanationLanguage: _fields[4].text.trim(),
        responseDetail: _detail,
        interests: interests,
      ),
    );
    if (saved && mounted) Navigator.of(context).pop();
  }

  Future<void> _clear() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('清空教练记忆？'),
        content: const Text('称呼、职业、语言与兴趣都会删除，对话和练习记录不受影响。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('清空'),
          ),
        ],
      ),
    );
    if (confirmed == true && await widget.controller.clear() && mounted) {
      Navigator.of(context).pop();
    }
  }
}

Map<String, Object> _encodeData(CoachingProfileData data) => <String, Object>{
  if (data.formOfAddress.isNotEmpty) 'form_of_address': data.formOfAddress,
  if (data.occupation.isNotEmpty) 'occupation': data.occupation,
  if (data.professionalContext.isNotEmpty)
    'professional_context': data.professionalContext,
  if (data.nativeLanguage.isNotEmpty) 'native_language': data.nativeLanguage,
  if (data.explanationLanguage.isNotEmpty)
    'explanation_language': data.explanationLanguage,
  if (data.responseDetail case final detail?)
    'response_detail': detail.name.toUpperCase(),
  if (data.interests.isNotEmpty) 'interests': data.interests,
};

CoachingProfileData _dataFromPatch(Map<String, Object> patch) =>
    CoachingProfileData(
      formOfAddress: patch['form_of_address'] as String? ?? '',
      occupation: patch['occupation'] as String? ?? '',
      professionalContext: patch['professional_context'] as String? ?? '',
      nativeLanguage: patch['native_language'] as String? ?? '',
      explanationLanguage: patch['explanation_language'] as String? ?? '',
      responseDetail: switch (patch['response_detail']) {
        'CONCISE' => CoachingResponseDetail.concise,
        'BALANCED' => CoachingResponseDetail.balanced,
        'DETAILED' => CoachingResponseDetail.detailed,
        _ => null,
      },
      interests:
          (patch['interests'] as List<Object>?)?.cast<String>() ?? const [],
    );

CoachingProfile _decodeProfile(String body) {
  final root = jsonDecode(body);
  if (root is! Map<String, Object?>) throw const FormatException();
  final raw = root['profile'];
  if (raw is! Map<String, Object?>) throw const FormatException();
  String text(String key) => raw[key] is String ? raw[key]! as String : '';
  final interests = raw['interests'];
  return CoachingProfile(
    memoryEnabled: root['memory_enabled'] as bool,
    version: root['version'] as int,
    data: CoachingProfileData(
      formOfAddress: text('form_of_address'),
      occupation: text('occupation'),
      professionalContext: text('professional_context'),
      nativeLanguage: text('native_language'),
      explanationLanguage: text('explanation_language'),
      responseDetail: switch (raw['response_detail']) {
        'CONCISE' => CoachingResponseDetail.concise,
        'BALANCED' => CoachingResponseDetail.balanced,
        'DETAILED' => CoachingResponseDetail.detailed,
        _ => null,
      },
      interests: interests is List<Object?>
          ? interests.whereType<String>().toList(growable: false)
          : const <String>[],
    ),
  );
}

String _profileSummary(CoachingProfileData data) {
  final values = <String>[
    if (data.formOfAddress.isNotEmpty) data.formOfAddress,
    if (data.occupation.isNotEmpty) data.occupation,
    ...data.interests.take(2),
  ];
  return values.isEmpty ? '已保存个性化偏好' : values.join(' · ');
}
