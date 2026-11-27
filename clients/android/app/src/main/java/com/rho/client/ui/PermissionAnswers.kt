package com.rho.client.ui

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.serialization.json.JsonElement

/**
 * The answers given to permission prompts, kept until the daemon has them.
 *
 * A phone loses its connection all the time: Android freezes a backgrounded
 * app, and the socket dies without anyone noticing until the next ping. An
 * answer given in that window used to go nowhere. With no connection it was
 * dropped on the spot and the prompt just sat there, so holding to approve
 * looked like it did nothing. With a dead socket that had not been noticed
 * yet, the send was queued to nobody and the prompt closed with the answer lost.
 *
 * The daemon makes both recoverable. It asks again on every reconnect with the
 * same request id, and it takes an answer by that id from any connection, even
 * a late one. So an answer is kept here: unsent until a connection takes it,
 * then remembered so that a prompt asked again gets the same answer
 * automatically instead of asking the person twice.
 */
class PermissionAnswers(private val remember: Int = 64) {

    data class Answer(val requestId: String, val rpcId: JsonElement, val approve: Boolean)

    private val answers = LinkedHashMap<String, Answer>()
    private val _unsent = MutableStateFlow<Set<String>>(emptySet())

    /** Request ids answered but not yet handed to a connection. */
    val unsent: StateFlow<Set<String>> = _unsent.asStateFlow()

    /** Records an answer. It stays unsent until [sent] is called for it. */
    @Synchronized
    fun record(requestId: String, rpcId: JsonElement, approve: Boolean) {
        answers.remove(requestId)
        answers[requestId] = Answer(requestId, rpcId, approve)
        while (answers.size > remember) {
            val oldest = answers.keys.first()
            answers.remove(oldest)
            _unsent.value = _unsent.value - oldest
        }
        _unsent.value = _unsent.value + requestId
    }

    /** The answers still waiting for a connection, oldest first. */
    @Synchronized
    fun pending(): List<Answer> = answers.values.filter { it.requestId in _unsent.value }

    /** A connection took the answer to [requestId]. */
    @Synchronized
    fun sent(requestId: String) {
        _unsent.value = _unsent.value - requestId
    }

    /**
     * The answer already given to a prompt the daemon is asking again, with the
     * id it is asking under now. Null when nobody has answered it here.
     */
    @Synchronized
    fun answerFor(requestId: String, rpcId: JsonElement): Answer? =
        answers[requestId]?.copy(rpcId = rpcId)?.also { answers[requestId] = it }
}
