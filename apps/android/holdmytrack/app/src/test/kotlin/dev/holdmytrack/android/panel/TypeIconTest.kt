package dev.holdmytrack.android.panel

import dev.holdmytrack.android.R
import org.junit.Assert.assertEquals
import org.junit.Test

class TypeIconTest {

    @Test
    fun `a solowheel or monowheel type has its own icon, any case, within the type`() {
        assertEquals(R.drawable.ic_monowheel, typeIcon("Solowheel"))
        assertEquals(R.drawable.ic_monowheel, typeIcon("monowheel_commute"))
    }

    @Test
    fun `every other type takes its kind's icon`() {
        assertEquals(R.drawable.ic_footprints, typeIcon("walking"))
        assertEquals(R.drawable.ic_bike, typeIcon("onewheel"))
        assertEquals(R.drawable.ic_bike, typeIcon("unicycle"))
        assertEquals(R.drawable.ic_car, typeIcon("driving"))
        assertEquals(R.drawable.ic_route, typeIcon("kayaking"))
    }
}
