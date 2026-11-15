/// Sky model — coordinate transforms and astronomical calculations.
///
/// Provides functions to convert between equatorial (RA/Dec) and horizontal
/// (Alt/Az) coordinate systems, compute sidereal time, and determine object
/// visibility from a given observer location and time.

pub mod render;

use std::f64::consts::PI;

const DEG2RAD: f64 = PI / 180.0;
const RAD2DEG: f64 = 180.0 / PI;

/// Altitude and azimuth of an object.
pub struct AltAz {
    /// Altitude in degrees (-90 = nadir, 0 = horizon, 90 = zenith).
    pub alt: f64,
    /// Azimuth in degrees (0 = North, 90 = East, 180 = South, 270 = West).
    pub az: f64,
}

/// Compute the Greenwich Mean Sidereal Time (GMST) in degrees for a given
/// Julian Date. Uses the IAU 1982 formula (accurate to ~1 arcsecond).
pub fn gmst_degrees(jd: f64) -> f64 {
    let t = (jd - 2451545.0) / 36525.0;
    let gmst = 280.46061837
        + 360.98564736629 * (jd - 2451545.0)
        + 0.000387933 * t * t
        - t * t * t / 38710000.0;
    gmst.rem_euclid(360.0)
}

/// Compute the Local Sidereal Time in degrees for a given Julian Date and
/// observer longitude (east positive, in degrees).
pub fn lst_degrees(jd: f64, longitude: f64) -> f64 {
    (gmst_degrees(jd) + longitude).rem_euclid(360.0)
}

/// Convert a calendar date/time (UTC) to Julian Date.
///
/// Uses a simplified formula valid for dates after 1582-10-15.
pub fn jd_from_utc(year: i32, month: u32, day: u32, hour: u32, minute: u32, second: f64) -> f64 {
    let (y, m) = if month <= 2 {
        (year as f64 - 1.0, month as f64 + 12.0)
    } else {
        (year as f64, month as f64)
    };
    let a = (y / 100.0).floor();
    let b = 2.0 - a + (a / 4.0).floor();
    let jd = (365.25 * (y + 4716.0)).floor()
        + (30.6001 * (m + 1.0)).floor()
        + day as f64
        + b
        - 1524.5
        + (hour as f64 + minute as f64 / 60.0 + second / 3600.0) / 24.0;
    jd
}

/// Convert equatorial coordinates (RA/Dec) to horizontal (Alt/Az) for a
/// given observer latitude and local sidereal time.
///
/// All inputs and outputs in degrees.
pub fn radec_to_altaz(ra: f64, dec: f64, lat: f64, lst: f64) -> AltAz {
    let ha = (lst - ra).rem_euclid(360.0); // hour angle in degrees

    let ha_r = ha * DEG2RAD;
    let dec_r = dec * DEG2RAD;
    let lat_r = lat * DEG2RAD;

    let sin_alt = dec_r.sin() * lat_r.sin() + dec_r.cos() * lat_r.cos() * ha_r.cos();
    let alt = sin_alt.asin();

    let cos_az = (dec_r.sin() - alt.sin() * lat_r.sin()) / (alt.cos() * lat_r.cos());
    let cos_az = cos_az.clamp(-1.0, 1.0); // guard against floating point errors
    let mut az = cos_az.acos();

    if ha_r.sin() > 0.0 {
        az = 2.0 * PI - az;
    }

    AltAz {
        alt: alt * RAD2DEG,
        az: az * RAD2DEG,
    }
}

/// Compute altitude of an object (convenience wrapper using current trip planner state).
pub fn object_altitude(ra: f64, dec: f64, lat: f64, lon: f64, jd: f64) -> f64 {
    let lst = lst_degrees(jd, lon);
    radec_to_altaz(ra, dec, lat, lst).alt
}
