package com.rho.client.ui

import com.rho.client.data.parseInstant
import com.rho.client.protocol.GoalStatus
import com.rho.client.protocol.RhoJson
import com.rho.client.protocol.RunStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/** The Run and Goal pages' working-out, on the runner's own example statuses. */
class StatusPagesTest {

    private fun fixture(name: String): String {
        var dir: File? = File("").absoluteFile
        while (dir != null && !File(dir, "clients/runs/testdata/status").isDirectory) dir = dir.parentFile
        requireNotNull(dir) { "could not find clients/runs/testdata/status" }
        return File(dir, "clients/runs/testdata/status/$name").readText()
    }

    private val run: RunStatus by lazy { RhoJson.decodeFromString(RunStatus.serializer(), fixture("run.json")) }
    private val goal: GoalStatus by lazy { RhoJson.decodeFromString(GoalStatus.serializer(), fixture("goal.json")) }

    private val minute = 60_000L

    @Test
    fun `a skipped check is not called met`() {
        assertEquals("Check: skipped", verdictLine(com.rho.client.protocol.Verdict(met = true, skipped = true)))
        assertEquals("Check: met", verdictLine(com.rho.client.protocol.Verdict(met = true)))
        assertEquals("Check: not met", verdictLine(com.rho.client.protocol.Verdict()))
    }

    @Test
    fun `a parked gate says it waits for Claude, and why`() {
        assertEquals("Waiting for Claude: You've hit your weekly limit", parkedLine("You've hit your weekly limit"))
        assertNull(parkedLine(""))
    }

    @Test
    fun `the timeline times each step from the one before`() {
        val now = parseInstant(run.since)!! + 42 * minute
        val rows = timeline(run, now)
        assertEquals(run.history.size + 1, rows.size)
        assertEquals(1_000L, rows[0].millis) // setup, a second after the start
        assertEquals(93 * minute - 1_000L, rows[1].millis)
        val failed = rows[1]
        assertEquals("work", failed.step)
        assertEquals("01M4P8ENR4XCTQT7HMWDVSCWG8", failed.session)
        assertEquals(false, failed.ok)
        assertTrue(failed.result.startsWith("the session ended paused"))
        assertEquals("1 blockers", rows[3].result)
        val current = rows.last()
        assertNull(current.ok)
        assertEquals("work", current.step)
        assertEquals(3, current.task)
        assertEquals(42 * minute, current.millis)
    }

    @Test
    fun `a finished run has no current row`() {
        val done = run.copy(step = "done")
        assertEquals(run.history.size, timeline(done, 0).size)
        assertEquals("done", stepLine(done, 0))
        assertEquals("failed at work", stepLine(run.copy(step = "failed", failedAt = "work"), 0))
    }

    @Test
    fun `the step line says the task, the try and the time in the step`() {
        val now = parseInstant(run.since)!! + 42 * minute
        assertEquals("work · task 3 of 3 · 42m in this step", stepLine(run, now))
        assertEquals("work · task 3 of 3 · try 2 · 42m in this step", stepLine(run.copy(attempt = 1), now))
        assertEquals("final-review · 42m in this step", stepLine(run.copy(step = "final-review", task = 0), now))
    }

    @Test
    fun `claude is counted at its gates`() {
        assertEquals(1, claudeGates(run))
    }

    @Test
    fun `rounds come newest first, with their briefs, verdicts and times`() {
        val rs = rounds(goal)
        assertEquals(listOf(2, 1), rs.map { it.round })
        assertEquals(listOf("In-Flight notes"), rs[0].briefs.map { it.title })
        assertNull(rs[0].verdict)
        assertNull(rs[0].millis)
        assertEquals(2, rs[1].briefs.size)
        assertEquals(false, rs[1].verdict?.met)
        assertEquals(4 * 60 * minute, rs[1].millis)
        assertEquals("round 2 of 10 · runs", goalLine(goal))
        assertEquals("merged", briefState(goal.briefs[0]))
        assertEquals("failed at work", briefState(goal.briefs[1]))
    }
}
